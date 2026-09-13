[CmdletBinding()]
param(
    [Parameter(Mandatory = $true, Position = 0)]
    [ValidatePattern('^[a-zA-Z0-9._-]+$')]
    [string]$Room,

    [string]$ServerUrl = 'http://mtes-pkg:8080',

    [switch]$Force
)

$ErrorActionPreference = 'Stop'
$ProgressPreference = 'SilentlyContinue'
$ServerUrl = $ServerUrl.TrimEnd('/')
$CacheRoot = Join-Path $env:ProgramData 'MTES\LocalDist\cache'
$script:RebootRequired = $false

$identity = [Security.Principal.WindowsIdentity]::GetCurrent()
$principal = [Security.Principal.WindowsPrincipal]::new($identity)
if (-not $principal.IsInRole([Security.Principal.WindowsBuiltInRole]::Administrator)) {
    Write-Error 'Run this deployment from an elevated Command Prompt or PowerShell window.'
    exit 1
}

function Test-PackageInstalled {
    param([object]$Package)

    if ($null -eq $Package.detect) {
        return $false
    }
    if ($Package.detect.type -eq 'file') {
        return Test-Path -LiteralPath ([Environment]::ExpandEnvironmentVariables($Package.detect.path)) -PathType Leaf
    }
    return $false
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
        default {
            throw "Unsupported package type '$($Package.type)'."
        }
    }

    if ($null -ne $process -and $process.ExitCode -notin @(0, 1641, 3010)) {
        throw "Installer for '$($Package.id)' exited with code $($process.ExitCode)."
    }
    if ($null -ne $process -and $process.ExitCode -in @(1641, 3010)) {
        $script:RebootRequired = $true
        Write-Warning "Installer for '$($Package.id)' requires a restart."
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
        }
        Update-PackagePath -Package $package
    }

    Write-Host 'Available deployment batch completed successfully.'
    if ($script:RebootRequired) {
        Write-Warning 'Restart Windows to finish installation.'
        exit 3010
    }
    exit 0
}
catch {
    Write-Error $_ -ErrorAction Continue
    exit 1
}
