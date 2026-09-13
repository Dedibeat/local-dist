[CmdletBinding()]
param(
    [string]$SourceRoot,

    [string]$InstallRoot = "$env:ProgramData\MTES\LocalDistServer",

    [string]$BinaryPath,

    [ValidatePattern('^[A-Za-z0-9._-]+$')]
    [string]$ShareName = 'software',

    [ValidatePattern('^[A-Za-z0-9._-]+$')]
    [string]$ShareUser = 'localdist-deploy',

    [SecureString]$SharePassword,

    [ValidateRange(1, 65535)]
    [int]$Port = 8080
)

$ErrorActionPreference = 'Stop'
$TaskName = 'MTES LocalDist Provider'
$HttpFirewallRule = 'MTES-LocalDist-HTTP'
$SmbFirewallRule = 'MTES-LocalDist-SMB'
$ShareDescription = 'MTES local-dist software'
$temporaryBinary = $null

function Assert-Administrator {
    $identity = [Security.Principal.WindowsIdentity]::GetCurrent()
    $principal = [Security.Principal.WindowsPrincipal]::new($identity)
    if (-not $principal.IsInRole([Security.Principal.WindowsBuiltInRole]::Administrator)) {
        throw 'Run this script from an elevated Windows PowerShell window.'
    }
}

function Resolve-FullPath {
    param([string]$Path)

    return [IO.Path]::GetFullPath([Environment]::ExpandEnvironmentVariables($Path))
}

function Ensure-ShareUser {
    param([string]$Name, [SecureString]$Password)

    $user = Get-LocalUser -Name $Name -ErrorAction SilentlyContinue
    if ($null -eq $user) {
        if ($null -eq $Password) {
            $Password = Read-Host "Password for the new local SMB account '$Name'" -AsSecureString
        }
        $user = New-LocalUser -Name $Name -Password $Password -Description 'Read-only MTES software deployment account' -PasswordNeverExpires -UserMayNotChangePassword
        Write-Host "Created local deployment account: $Name"
    }
    elseif (-not $user.Enabled) {
        Enable-LocalUser -Name $Name
        Write-Host "Enabled local deployment account: $Name"
    }
    else {
        Write-Host "Using existing local deployment account: $Name"
    }
}

function Copy-ProviderData {
    param([string]$From, [string]$To)

    $sourceData = Join-Path $From 'data'
    $targetData = Join-Path $To 'data'
    $targetScripts = Join-Path $To 'scripts'
    New-Item -ItemType Directory -Force -Path $targetData, $targetScripts | Out-Null
    Copy-Item -LiteralPath (Join-Path $sourceData 'catalog.json') -Destination $targetData -Force
    Copy-Item -LiteralPath (Join-Path $sourceData 'rooms.json') -Destination $targetData -Force
    Copy-Item -Path (Join-Path $From 'scripts\*') -Destination $targetScripts -Recurse -Force

    $sourcePackages = Join-Path $sourceData 'packages'
    $targetPackages = Join-Path $targetData 'packages'
    New-Item -ItemType Directory -Force -Path $targetPackages | Out-Null
    & robocopy.exe $sourcePackages $targetPackages /E /COPY:DAT /DCOPY:DAT /R:2 /W:1 /NFL /NDL /NJH /NJS /NP
    if ($LASTEXITCODE -ge 8) {
        throw "Could not copy package payloads; robocopy exited with code $LASTEXITCODE."
    }
}

function Set-FirewallRule {
    param([string]$Name, [int]$LocalPort)

    Get-NetFirewallRule -Name $Name -ErrorAction SilentlyContinue | Remove-NetFirewallRule
    New-NetFirewallRule -Name $Name -DisplayName $Name -Direction Inbound -Action Allow -Protocol TCP -LocalPort $LocalPort -RemoteAddress LocalSubnet -Profile Any | Out-Null
}

function Stop-InstalledProvider {
    param([string]$Root)

    $task = Get-ScheduledTask -TaskName $TaskName -ErrorAction SilentlyContinue
    if ($null -ne $task) {
        Stop-ScheduledTask -TaskName $TaskName -ErrorAction SilentlyContinue
    }

    $installedBinary = Join-Path $Root 'local-dist.exe'
    Get-CimInstance Win32_Process -Filter "Name = 'local-dist.exe'" -ErrorAction SilentlyContinue |
        Where-Object { $null -ne $_.ExecutablePath -and $_.ExecutablePath -eq $installedBinary } |
        ForEach-Object { Invoke-CimMethod -InputObject $_ -MethodName Terminate | Out-Null }
}

Assert-Administrator

if ([string]::IsNullOrWhiteSpace($SourceRoot)) {
    $SourceRoot = Split-Path -Parent $PSScriptRoot
}
$SourceRoot = Resolve-FullPath $SourceRoot
$InstallRoot = Resolve-FullPath $InstallRoot
if ($InstallRoot.Contains('"')) {
    throw 'InstallRoot cannot contain a quotation mark.'
}

foreach ($required in @('data\catalog.json', 'data\rooms.json', 'data\packages', 'scripts\setup.cmd', 'scripts\install.ps1')) {
    if (-not (Test-Path -LiteralPath (Join-Path $SourceRoot $required))) {
        throw "SourceRoot is incomplete; missing $required"
    }
}
$existingShare = Get-SmbShare -Name $ShareName -ErrorAction SilentlyContinue
if ($null -ne $existingShare -and ((Resolve-FullPath $existingShare.Path) -ne $InstallRoot -or $existingShare.Description -ne $ShareDescription)) {
    throw "SMB share '$ShareName' already exists and is not managed by local-dist. Choose another -ShareName or remove that share first."
}

try {
    if ([string]::IsNullOrWhiteSpace($BinaryPath)) {
        $go = Get-Command go.exe -ErrorAction SilentlyContinue
        if ($null -eq $go) {
            throw 'Go is not installed or not on PATH. Install Go 1.23 or newer, or pass -BinaryPath with a Windows local-dist.exe build.'
        }
        $temporaryBinary = Join-Path $env:TEMP ("local-dist-{0}.exe" -f [Guid]::NewGuid().ToString('N'))
        Write-Host 'Building Windows provider...'
        Push-Location $SourceRoot
        try {
            & $go.Source build -trimpath -o $temporaryBinary ./cmd/local-dist
            if ($LASTEXITCODE -ne 0) {
                throw "go build exited with code $LASTEXITCODE."
            }
        }
        finally {
            Pop-Location
        }
        $BinaryPath = $temporaryBinary
    }
    else {
        $BinaryPath = Resolve-FullPath $BinaryPath
    }
    if (-not (Test-Path -LiteralPath $BinaryPath -PathType Leaf)) {
        throw "Provider executable is missing: $BinaryPath"
    }

    Ensure-ShareUser -Name $ShareUser -Password $SharePassword
    $shareAccount = "$env:COMPUTERNAME\$ShareUser"
    New-Item -ItemType Directory -Force -Path $InstallRoot | Out-Null
    & icacls.exe $InstallRoot /grant:r "${shareAccount}:(OI)(CI)RX" /T /C | Out-Null
    if ($LASTEXITCODE -ne 0) {
        throw "Could not grant read access to $shareAccount."
    }

    Stop-InstalledProvider -Root $InstallRoot

    Write-Host "Copying provider files to $InstallRoot..."
    Copy-ProviderData -From $SourceRoot -To $InstallRoot
    Copy-Item -LiteralPath $BinaryPath -Destination (Join-Path $InstallRoot 'local-dist.exe') -Force

    if ($null -eq $existingShare) {
        $administrators = ([Security.Principal.SecurityIdentifier]'S-1-5-32-544').Translate([Security.Principal.NTAccount]).Value
        New-SmbShare -Name $ShareName -Path $InstallRoot -Description $ShareDescription -ReadAccess $shareAccount -FullAccess $administrators -CachingMode None -FolderEnumerationMode AccessBased | Out-Null
        Write-Host "Created read-only SMB share: \\$env:COMPUTERNAME\$ShareName"
    }
    else {
        Grant-SmbShareAccess -Name $ShareName -AccountName $shareAccount -AccessRight Read -Force | Out-Null
        Write-Host "Updated SMB share access: \\$env:COMPUTERNAME\$ShareName"
    }

    Set-FirewallRule -Name $HttpFirewallRule -LocalPort $Port
    Set-FirewallRule -Name $SmbFirewallRule -LocalPort 445

    $logDirectory = Join-Path $InstallRoot 'logs'
    New-Item -ItemType Directory -Force -Path $logDirectory | Out-Null
    $installedBinary = Join-Path $InstallRoot 'local-dist.exe'
    $providerArguments = '-listen ":{0}" -data "{1}" -log "{2}"' -f $Port, (Join-Path $InstallRoot 'data'), (Join-Path $logDirectory 'provider.log')
    $action = New-ScheduledTaskAction -Execute $installedBinary -Argument $providerArguments
    $trigger = New-ScheduledTaskTrigger -AtStartup
    $principal = New-ScheduledTaskPrincipal -UserId 'SYSTEM' -LogonType ServiceAccount -RunLevel Highest
    $settings = New-ScheduledTaskSettingsSet -StartWhenAvailable -RestartCount 3 -RestartInterval (New-TimeSpan -Minutes 1) -ExecutionTimeLimit ([TimeSpan]::Zero) -MultipleInstances IgnoreNew
    Register-ScheduledTask -TaskName $TaskName -Action $action -Trigger $trigger -Principal $principal -Settings $settings -Description 'Serves MTES room plans and Windows installer payloads.' -Force | Out-Null
    Start-ScheduledTask -TaskName $TaskName

    $healthy = $false
    for ($attempt = 1; $attempt -le 20; $attempt++) {
        Start-Sleep -Milliseconds 500
        try {
            $response = Invoke-RestMethod -Uri "http://127.0.0.1:$Port/healthz" -TimeoutSec 2
            if ($response.status -eq 'ok') {
                $healthy = $true
                break
            }
        }
        catch {
        }
    }
    if (-not $healthy) {
        throw "Provider did not become healthy. Read $(Join-Path $InstallRoot 'logs\provider.log') and check the '$TaskName' scheduled task."
    }
    $runningTask = Get-ScheduledTask -TaskName $TaskName
    if ($runningTask.State -ne 'Running') {
        throw "The health port responded, but the '$TaskName' scheduled task is not running. Port $Port may belong to another program."
    }
    $catalog = Invoke-RestMethod -Uri "http://127.0.0.1:$Port/api/v1/catalog" -TimeoutSec 5
    $unavailablePackages = @($catalog.packages | Where-Object { -not $_.available })
    if ($unavailablePackages.Count -gt 0) {
        Write-Warning "$($unavailablePackages.Count) catalog package(s) have no local payload and will not be deployable from this server."
    }

    Write-Host ''
    Write-Host 'Windows provider is ready.'
    Write-Host "Health: http://$env:COMPUTERNAME`:$Port/healthz"
    Write-Host "Connect: net use Z: \\$env:COMPUTERNAME\$ShareName /user:$shareAccount *"
    Write-Host "Deploy: Z:\scripts\setup.cmd room-208 -ServerUrl http://$env:COMPUTERNAME`:$Port"
}
finally {
    if ($null -ne $temporaryBinary) {
        Remove-Item -LiteralPath $temporaryBinary -Force -ErrorAction SilentlyContinue
    }
}
