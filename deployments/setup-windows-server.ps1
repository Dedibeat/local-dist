[CmdletBinding()]
param(
    [string]$SourceRoot,

    [string]$InstallRoot = "$env:ProgramData\MTES\LocalDistServer",

    [ValidatePattern('^[A-Za-z0-9._-]+$')]
    [string]$ShareName = 'software',

    [ValidatePattern('^[A-Za-z0-9._-]+$')]
    [string]$ShareUser = 'localdist-deploy',

    [SecureString]$SharePassword,

    [ValidateRange(1, 65535)]
    [int]$Port = 8080,

    [switch]$SkipPackageDownload
)

$ErrorActionPreference = 'Stop'
$ProgressPreference = 'SilentlyContinue'
$MinimumGoMajor = 1
$MinimumGoMinor = 23
$ToolRoot = Join-Path $env:ProgramData 'MTES\LocalDistTools'

function Assert-Administrator {
    $identity = [Security.Principal.WindowsIdentity]::GetCurrent()
    $principal = [Security.Principal.WindowsPrincipal]::new($identity)
    if (-not $principal.IsInRole([Security.Principal.WindowsBuiltInRole]::Administrator)) {
        throw 'Run server.cmd from an Administrator Command Prompt.'
    }
}

function Test-GoVersion {
    param([string]$GoExecutable)

    try {
        $version = (& $GoExecutable env GOVERSION 2>$null).Trim()
        if ($LASTEXITCODE -ne 0 -or $version -notmatch '^go(?<major>\d+)\.(?<minor>\d+)') {
            return $false
        }
        $major = [int]$Matches.major
        $minor = [int]$Matches.minor
        return $major -gt $MinimumGoMajor -or ($major -eq $MinimumGoMajor -and $minor -ge $MinimumGoMinor)
    }
    catch {
        return $false
    }
}

function Find-CompatibleGo {
    $candidates = @()
    $command = Get-Command go.exe -ErrorAction SilentlyContinue
    if ($null -ne $command) {
        $candidates += $command.Source
    }
    $programFilesGo = Join-Path $env:ProgramFiles 'Go\bin\go.exe'
    if (Test-Path -LiteralPath $programFilesGo -PathType Leaf) {
        $candidates += $programFilesGo
    }
    if (Test-Path -LiteralPath $ToolRoot -PathType Container) {
        $candidates += @(Get-ChildItem -LiteralPath $ToolRoot -Filter go.exe -File -Recurse -ErrorAction SilentlyContinue | ForEach-Object FullName)
    }

    foreach ($candidate in $candidates | Select-Object -Unique) {
        if (Test-GoVersion -GoExecutable $candidate) {
            return $candidate
        }
    }
    return $null
}

function Install-PortableGo {
    if (-not [Environment]::Is64BitOperatingSystem) {
        throw 'The automatic server bootstrap currently supports 64-bit Windows only.'
    }

    [Net.ServicePointManager]::SecurityProtocol = [Net.ServicePointManager]::SecurityProtocol -bor [Net.SecurityProtocolType]::Tls12
    Write-Host 'Finding the current stable Go toolchain...'
    $releases = Invoke-RestMethod -Uri 'https://go.dev/dl/?mode=json' -TimeoutSec 60
    $release = @($releases | Where-Object { $_.stable }) | Select-Object -First 1
    if ($null -eq $release) {
        throw 'The official Go release service did not return a stable release.'
    }
    $archive = @($release.files | Where-Object { $_.os -eq 'windows' -and $_.arch -eq 'amd64' -and $_.kind -eq 'archive' }) | Select-Object -First 1
    if ($null -eq $archive -or $archive.filename -notmatch '^go[0-9.]+\.windows-amd64\.zip$' -or $archive.sha256 -notmatch '^[0-9a-fA-F]{64}$') {
        throw 'The official Go release metadata has no valid Windows amd64 archive.'
    }

    $downloadDirectory = Join-Path $ToolRoot 'downloads'
    $versionDirectory = Join-Path $ToolRoot $release.version
    $goExecutable = Join-Path $versionDirectory 'go\bin\go.exe'
    New-Item -ItemType Directory -Force -Path $downloadDirectory, $versionDirectory | Out-Null
    $archivePath = Join-Path $downloadDirectory $archive.filename

    $archiveReady = $false
    if (Test-Path -LiteralPath $archivePath -PathType Leaf) {
        $archiveReady = (Get-FileHash -LiteralPath $archivePath -Algorithm SHA256).Hash -eq $archive.sha256
    }
    if (-not $archiveReady) {
        $partialPath = "$archivePath.partial"
        Write-Host "Downloading $($archive.filename)..."
        try {
            Invoke-WebRequest -UseBasicParsing -Uri ("https://go.dev/dl/{0}" -f $archive.filename) -OutFile $partialPath -TimeoutSec 1800
            $actualHash = (Get-FileHash -LiteralPath $partialPath -Algorithm SHA256).Hash
            if ($actualHash -ne $archive.sha256) {
                throw "Go archive checksum mismatch: expected $($archive.sha256), got $actualHash."
            }
            Move-Item -LiteralPath $partialPath -Destination $archivePath -Force
        }
        finally {
            Remove-Item -LiteralPath $partialPath -Force -ErrorAction SilentlyContinue
        }
    }

    if (-not (Test-GoVersion -GoExecutable $goExecutable)) {
        Write-Host "Installing private Go toolchain $($release.version)..."
        Expand-Archive -LiteralPath $archivePath -DestinationPath $versionDirectory -Force
    }
    if (-not (Test-GoVersion -GoExecutable $goExecutable)) {
        throw "The private Go toolchain did not install correctly: $goExecutable"
    }
    return $goExecutable
}

Assert-Administrator
if ([string]::IsNullOrWhiteSpace($SourceRoot)) {
    $SourceRoot = Split-Path -Parent $PSScriptRoot
}
$SourceRoot = [IO.Path]::GetFullPath($SourceRoot)
$installer = Join-Path $SourceRoot 'deployments\install-windows-server.ps1'
foreach ($required in @($installer, (Join-Path $SourceRoot 'go.mod'), (Join-Path $SourceRoot 'cmd\local-dist'), (Join-Path $SourceRoot 'cmd\fetch-packages'), (Join-Path $SourceRoot 'data\catalog.json'))) {
    if (-not (Test-Path -LiteralPath $required)) {
        throw "Server source is incomplete; missing $required"
    }
}

$goExecutable = Find-CompatibleGo
if ($null -eq $goExecutable) {
    $goExecutable = Install-PortableGo
}
Write-Host "Using Go: $goExecutable"
$env:Path = "$(Split-Path -Parent $goExecutable);$env:Path"

Push-Location $SourceRoot
try {
    if (-not $SkipPackageDownload) {
        Write-Host 'Downloading and verifying catalog packages...'
        & $goExecutable run ./cmd/fetch-packages -data ./data
        if ($LASTEXITCODE -ne 0) {
            throw "Package download and verification exited with code $LASTEXITCODE."
        }
    }

    $installArguments = @{
        SourceRoot = $SourceRoot
        InstallRoot = $InstallRoot
        ShareName = $ShareName
        ShareUser = $ShareUser
        Port = $Port
    }
    if ($null -ne $SharePassword) {
        $installArguments.SharePassword = $SharePassword
    }
    & $installer @installArguments
}
finally {
    Pop-Location
}
