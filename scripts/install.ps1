[CmdletBinding()]
param(
    [Parameter(Mandatory = $true, Position = 0)]
    [ValidatePattern('^[a-zA-Z0-9._-]+$')]
    [string]$Room,

    [string]$ServerUrl = 'http://mtes-pkg:8080',

    [switch]$Force,

    [string]$LogPath
)

$ErrorActionPreference = 'Stop'
$ProgressPreference = 'SilentlyContinue'
$ServerUrl = $ServerUrl.TrimEnd('/')
$CacheRoot = Join-Path $env:ProgramData 'MTES\LocalDist\cache'
$script:RebootRequired = $false
$script:TranscriptStarted = $false
$script:LogAvailable = $false
$exitCode = 1

if ([string]::IsNullOrWhiteSpace($LogPath)) {
    $computerName = $env:COMPUTERNAME
    if ([string]::IsNullOrWhiteSpace($computerName)) {
        $computerName = 'computer'
    }
    $LogPath = Join-Path $env:ProgramData ('MTES\LocalDist\logs\setup-{0}-{1}.log' -f $computerName, (Get-Date -Format 'yyyyMMdd-HHmmss'))
}

$identity = [Security.Principal.WindowsIdentity]::GetCurrent()
$principal = [Security.Principal.WindowsPrincipal]::new($identity)
if (-not $principal.IsInRole([Security.Principal.WindowsBuiltInRole]::Administrator)) {
    Write-Error 'Run this deployment from an elevated Command Prompt or PowerShell window.'
    exit 1
}

function Start-DeploymentLog {
    param([string]$Path)

    try {
        $directory = Split-Path -Parent -Path $Path
        if (-not [string]::IsNullOrWhiteSpace($directory)) {
            New-Item -ItemType Directory -Force -Path $directory | Out-Null
        }
        Start-Transcript -Path $Path -Append -ErrorAction Stop | Out-Null
        $script:TranscriptStarted = $true
        $script:LogAvailable = $true
        Write-Host "Deployment log: $Path"
    }
    catch {
        Write-Warning "Could not start deployment log at '$Path': $($_.Exception.Message)"
    }
}

function Stop-DeploymentLog {
    if (-not $script:TranscriptStarted) {
        return
    }
    try {
        Stop-Transcript | Out-Null
    }
    catch {
        Write-Warning "Could not finish deployment log at '$LogPath': $($_.Exception.Message)"
    }
    $script:TranscriptStarted = $false
}

Start-DeploymentLog -Path $LogPath

function Test-PackageInstalled {
    param([object]$Package)

    if ($null -eq $Package.detect) {
        Write-Host "Detection for $($Package.id): no file rule"
        return $false
    }
    if ($Package.detect.type -eq 'file') {
        $path = [Environment]::ExpandEnvironmentVariables($Package.detect.path)
        $installed = Test-Path -LiteralPath $path -PathType Leaf
        Write-Host "Detection for $($Package.id): $installed ($path)"
        return $installed
    }
    if ($Package.detect.type -eq 'command') {
        $path = [Environment]::ExpandEnvironmentVariables($Package.detect.path)
        if (-not (Test-Path -LiteralPath $path -PathType Leaf)) {
            Write-Host "Detection for $($Package.id): False (command is missing: $path)"
            return $false
        }

        $startOptions = @{
            FilePath = $path
            Wait = $true
            PassThru = $true
        }
        if ($null -ne $Package.detect.args -and @($Package.detect.args).Count -gt 0) {
            $startOptions.ArgumentList = @($Package.detect.args)
        }
        try {
            $process = Start-Process @startOptions
            $installed = $process.ExitCode -eq 0
            Write-Host "Detection for $($Package.id): $installed (command exit code $($process.ExitCode): $path)"
            return $installed
        }
        catch {
            Write-Host "Detection for $($Package.id): False (command failed: $path)"
            return $false
        }
    }
    Write-Host "Detection for $($Package.id): unsupported rule type '$($Package.detect.type)'"
    return $false
}

function Assert-PackageInstalled {
    param([object]$Package)

    if ($null -eq $Package.detect) {
        throw "Package '$($Package.id)' has no detection rule; installation cannot be verified."
    }

    $attempts = 5
    for ($attempt = 1; $attempt -le $attempts; $attempt++) {
        if (Test-PackageInstalled -Package $Package) {
            return
        }
        if ($attempt -lt $attempts) {
            Write-Host "Verification for '$($Package.id)' is not ready; retrying ($attempt/$attempts)..."
            Start-Sleep -Seconds 1
        }
    }

    $detectPath = [Environment]::ExpandEnvironmentVariables($Package.detect.path)
    throw "Installer for '$($Package.id)' exited successfully, but its '$($Package.detect.type)' verification failed after $attempts attempts: $detectPath"
}

function Get-PackageFile {
    param([object]$Package)

    if (-not $Package.available) {
        throw "Package '$($Package.id)' is in the catalog, but its installer is missing on the server."
    }

    $packageCache = Join-Path $CacheRoot $Package.id
    New-Item -ItemType Directory -Force -Path $packageCache | Out-Null
    $fileName = [IO.Path]::GetFileName($Package.source)
    $target = Join-Path $packageCache $fileName
    $expectedHash = $Package.sha256.ToLowerInvariant()

    if (Test-Path -LiteralPath $target) {
        $existingHash = (Get-FileHash -LiteralPath $target -Algorithm SHA256).Hash.ToLowerInvariant()
        if ($existingHash -eq $expectedHash) {
            return $target
        }
        Remove-Item -LiteralPath $target -Force
    }

    $downloadUrl = $ServerUrl + $Package.downloadUrl
    Write-Host "Downloading $($Package.name) $($Package.version)..."
    Invoke-WebRequest -UseBasicParsing -Uri $downloadUrl -OutFile $target

    $actualHash = (Get-FileHash -LiteralPath $target -Algorithm SHA256).Hash.ToLowerInvariant()
    if ($actualHash -ne $expectedHash) {
        Remove-Item -LiteralPath $target -Force
        throw "Checksum mismatch for '$($Package.id)'. Expected $expectedHash, received $actualHash."
    }
    return $target
}

function Install-Package {
    param([object]$Package, [string]$Installer)

    if (-not [string]::IsNullOrWhiteSpace($Package.install.uninstallBeforeInstall)) {
        $productCode = $Package.install.uninstallBeforeInstall
        Write-Host "Removing the existing $($Package.name) installation before migration..."
        $removeProcess = Start-Process -FilePath 'msiexec.exe' -ArgumentList @('/x', $productCode, '/qn', '/norestart') -Wait -PassThru
        Write-Host "Uninstaller exit code for $($Package.id): $($removeProcess.ExitCode)"
        if ($removeProcess.ExitCode -notin @(0, 1605, 1614, 1641, 3010)) {
            throw "Uninstaller for '$($Package.id)' exited with code $($removeProcess.ExitCode)."
        }
        if ($removeProcess.ExitCode -in @(1641, 3010)) {
            $script:RebootRequired = $true
        }
    }

    Write-Host "Installing $($Package.name) $($Package.version)..."
    $process = $null
    switch ($Package.type) {
        'msi' {
            $quotedInstaller = '"{0}"' -f $Installer
            $arguments = @('/i', $quotedInstaller, '/qn', '/norestart')
            if ($null -ne $Package.install.args) {
                $arguments += @($Package.install.args)
            }
            $process = Start-Process -FilePath 'msiexec.exe' -ArgumentList $arguments -Wait -PassThru
        }
        'exe' {
            $startOptions = @{
                FilePath = $Installer
                Wait = $true
                PassThru = $true
            }
            if ($null -ne $Package.install.args -and @($Package.install.args).Count -gt 0) {
                $startOptions.ArgumentList = @($Package.install.args)
            }
            $process = Start-Process @startOptions
        }
        'zip' {
            $destination = [Environment]::ExpandEnvironmentVariables($Package.install.destination)
            New-Item -ItemType Directory -Force -Path $destination | Out-Null
            Expand-Archive -LiteralPath $Installer -DestinationPath $destination -Force
        }
        'portable' {
            $destination = [Environment]::ExpandEnvironmentVariables($Package.install.destination)
            New-Item -ItemType Directory -Force -Path $destination | Out-Null
            Copy-Item -LiteralPath $Installer -Destination $destination -Force
        }
        'npm' {
            $npm = Join-Path $env:ProgramFiles 'nodejs\npm.cmd'
            if (-not (Test-Path -LiteralPath $npm -PathType Leaf)) {
                throw "Package '$($Package.id)' requires Node.js and npm."
            }
            $process = Start-Process -FilePath $npm -ArgumentList @('install', '--global', '--offline', '--no-audit', '--no-fund', $Installer) -Wait -PassThru
        }
        default {
            throw "Unsupported package type '$($Package.type)'."
        }
    }

    if ($null -ne $process -and $process.ExitCode -notin @(0, 1641, 3010)) {
        throw "Installer for '$($Package.id)' exited with code $($process.ExitCode)."
    }
    if ($null -ne $process) {
        Write-Host "Installer exit code for $($Package.id): $($process.ExitCode)"
    }
    if ($null -ne $process -and $process.ExitCode -in @(1641, 3010)) {
        $script:RebootRequired = $true
        Write-Warning "Installer for '$($Package.id)' requires a restart."
    }
}

function Ensure-StartMenuShortcut {
    param([object]$Package)

    if ($null -eq $Package.install.startMenu) {
        return
    }
    if ($null -eq $Package.detect -or $Package.detect.type -ne 'file') {
        Write-Warning "Cannot create a Start-menu shortcut for '$($Package.id)' without a file detection rule."
        return
    }

    $target = [Environment]::ExpandEnvironmentVariables($Package.detect.path)
    if (-not (Test-Path -LiteralPath $target -PathType Leaf)) {
        Write-Warning "Cannot create a Start-menu shortcut for '$($Package.id)': target is missing ($target)."
        return
    }

    $programs = Join-Path $env:ProgramData 'Microsoft\Windows\Start Menu\Programs'
    $folder = Join-Path $programs $Package.install.startMenu.folder
    $shortcutPath = Join-Path $folder $Package.install.startMenu.name
    $shell = $null
    $shortcut = $null
    try {
        New-Item -ItemType Directory -Force -Path $folder | Out-Null
        $shell = New-Object -ComObject WScript.Shell
        $shortcut = $shell.CreateShortcut($shortcutPath)
        $shortcut.TargetPath = $target
        $shortcut.WorkingDirectory = Split-Path -Parent -Path $target
        $shortcut.Description = $Package.name
        $shortcut.Save()
        Write-Host "Start-menu shortcut ready: $shortcutPath -> $target"
    }
    catch {
        Write-Warning "Could not create Start-menu shortcut for '$($Package.id)': $($_.Exception.Message)"
    }
    finally {
        if ($null -ne $shortcut) {
            [void][Runtime.InteropServices.Marshal]::ReleaseComObject($shortcut)
        }
        if ($null -ne $shell) {
            [void][Runtime.InteropServices.Marshal]::ReleaseComObject($shell)
        }
    }
}

function Update-PackagePath {
    param([object]$Package)

    if ($null -ne $Package.install.addToPath) {
        foreach ($entry in @($Package.install.addToPath)) {
            $expandedEntry = [Environment]::ExpandEnvironmentVariables($entry).TrimEnd('\')
            $machinePath = [Environment]::GetEnvironmentVariable('Path', 'Machine')
            $pathEntries = @($machinePath -split ';' | ForEach-Object { $_.TrimEnd('\') })
            if ($pathEntries -notcontains $expandedEntry) {
                [Environment]::SetEnvironmentVariable('Path', "$machinePath;$expandedEntry", 'Machine')
            }
            if (@($env:Path -split ';' | ForEach-Object { $_.TrimEnd('\') }) -notcontains $expandedEntry) {
                $env:Path = "$env:Path;$expandedEntry"
            }
        }
    }
}

try {
    New-Item -ItemType Directory -Force -Path $CacheRoot | Out-Null
    $planUrl = "$ServerUrl/api/v1/rooms/$($Room.ToLowerInvariant())"
    $plan = Invoke-RestMethod -UseBasicParsing -Uri $planUrl
    Write-Host "Applying package plan for $($plan.name) ($($plan.packages.Count) packages)."

    if (-not $plan.complete) {
        Write-Warning "This is a partial room plan. $($plan.pending.Count) requirement groups are still pending."
    }

    foreach ($package in $plan.packages) {
        if (-not $Force -and (Test-PackageInstalled -Package $package)) {
            Write-Host "Already installed: $($package.name)"
        }
        else {
            $installer = Get-PackageFile -Package $package
            Install-Package -Package $package -Installer $installer
            Assert-PackageInstalled -Package $package
        }
        Update-PackagePath -Package $package
        Ensure-StartMenuShortcut -Package $package
    }

    Write-Host 'Available deployment batch completed successfully.'
    if ($script:RebootRequired) {
        Write-Warning 'Restart Windows to finish installation.'
        $exitCode = 3010
    }
    else {
        $exitCode = 0
    }
}
catch {
    Write-Error $_ -ErrorAction Continue
    $exitCode = 1
}
finally {
    Stop-DeploymentLog
}

if ($script:LogAvailable) {
    Write-Host "Deployment log saved to: $LogPath"
}
else {
    Write-Warning "Deployment log was not saved. Requested path: $LogPath"
}
exit $exitCode
