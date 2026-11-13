# FrankenManager installer (Windows)
# Usage, in PowerShell:
#   irm https://raw.githubusercontent.com/leobsst/frankenphp-cli/refs/heads/main/install.ps1 | iex
#
# Environment:
#   VERSION                     version to install (default: the latest release)
#   FRANKENMANAGER_INSTALL_DIR  install directory
#                               (default: %LOCALAPPDATA%\Programs\FrankenManager)
#   DOWNLOAD_BASE               where the release assets are (default: the GitHub
#                               release of VERSION); a local directory works, for tests
#
# No Administrator rights are needed to install or update. FrankenManager
# itself needs an Administrator terminal whenever it changes the hosts file.

# Everything runs in its own scope: under `irm | iex`, nothing is left in the
# caller's session (variables, function, error preference).
& {
    $ErrorActionPreference = 'Stop'
    $ProgressPreference = 'SilentlyContinue' # Invoke-WebRequest is much faster without it

    $Repo = 'leobsst/frankenphp-cli'
    $BinaryName = 'frankenmanager'

    # x64 PowerShell on an ARM64 machine reports AMD64: PROCESSOR_ARCHITEW6432
    # holds the real architecture then
    $Arch = if ($env:PROCESSOR_ARCHITEW6432) { $env:PROCESSOR_ARCHITEW6432 } else { $env:PROCESSOR_ARCHITECTURE }
    switch ($Arch) {
        'AMD64' { $Platform = 'windows-x86_64' }
        'ARM64' { $Platform = 'windows-arm64' }
        default { throw "Unsupported architecture: $Arch" }
    }
    $Asset = "$BinaryName-$Platform.tar.gz"

    $Version = $env:VERSION
    if (-not $Version) {
        $Latest = Invoke-RestMethod -Uri "https://api.github.com/repos/$Repo/releases/latest" -Headers @{ 'User-Agent' = 'FrankenManager-installer' }
        $Version = $Latest.tag_name
        if (-not $Version) { throw 'Could not determine latest version. Set the VERSION environment variable to override.' }
    }
    $Version = $Version.TrimStart('v')

    $DownloadBase = $env:DOWNLOAD_BASE
    if (-not $DownloadBase) { $DownloadBase = "https://github.com/$Repo/releases/download/v$Version" }

    $InstallDir = $env:FRANKENMANAGER_INSTALL_DIR
    if (-not $InstallDir) { $InstallDir = Join-Path $env:LOCALAPPDATA 'Programs\FrankenManager' }

    Write-Host "Installing $BinaryName v$Version ($Asset)..."

    $Tmp = Join-Path ([System.IO.Path]::GetTempPath()) ("frankenmanager-install-" + [guid]::NewGuid())
    New-Item -ItemType Directory -Path $Tmp | Out-Null
    try {
        # A URL or a local directory
        function Get-Asset([string]$Name) {
            $Dest = Join-Path $Tmp $Name
            if ($DownloadBase -match '^https?://') {
                Invoke-WebRequest -Uri "$DownloadBase/$Name" -OutFile $Dest -UseBasicParsing
            } else {
                Copy-Item -Path (Join-Path $DownloadBase $Name) -Destination $Dest
            }
            return $Dest
        }

        $Archive = Get-Asset $Asset

        # Check the archive against the published checksums before installing it
        $Sums = $null
        try { $Sums = Get-Asset 'SHA256SUMS.txt' } catch { Write-Warning "No SHA256SUMS.txt for v${Version}: the archive is not verified." }
        if ($Sums) {
            $Line = Get-Content $Sums | Where-Object { ($_ -split '\s+')[1] -eq $Asset } | Select-Object -First 1
            $Expected = if ($Line) { ($Line -split '\s+')[0].ToLower() } else { '' }
            $Actual = (Get-FileHash -Algorithm SHA256 -Path $Archive).Hash.ToLower()
            if (-not $Expected -or $Expected -ne $Actual) {
                throw "Checksum mismatch for ${Asset}: the download is corrupted or was tampered with."
            }
        }

        # tar.exe ships with Windows 10 1803 and later
        & tar.exe -xzf $Archive -C $Tmp
        if ($LASTEXITCODE -ne 0) { throw "Could not extract $Asset." }
        $NewExe = Join-Path $Tmp "$BinaryName\$BinaryName.exe"
        if (-not (Test-Path $NewExe -PathType Leaf)) { throw "The archive does not contain $BinaryName/$BinaryName.exe." }

        New-Item -ItemType Directory -Path $InstallDir -Force | Out-Null
        $Exe = Join-Path $InstallDir "$BinaryName.exe"
        $Old = "$Exe.old"
        # A running executable cannot be replaced, but can be renamed: move it
        # aside, like `frankenmanager update` does
        if (Test-Path $Old) { Remove-Item $Old -Force -ErrorAction SilentlyContinue }
        if (Test-Path $Exe) { Move-Item -Path $Exe -Destination $Old -Force }
        Move-Item -Path $NewExe -Destination $Exe
        if (Test-Path $Old) { Remove-Item $Old -Force -ErrorAction SilentlyContinue }
    } finally {
        Remove-Item -Recurse -Force $Tmp -ErrorAction SilentlyContinue
    }

    # Put the install directory on the user's PATH
    $UserPath = [Environment]::GetEnvironmentVariable('Path', 'User')
    $Entries = if ($UserPath) { $UserPath -split ';' } else { @() }
    if ($Entries -notcontains $InstallDir) {
        $NewPath = (@($Entries | Where-Object { $_ }) + $InstallDir) -join ';'
        [Environment]::SetEnvironmentVariable('Path', $NewPath, 'User')
        Write-Host "Added $InstallDir to your PATH: open a new terminal to use it."
    }
    if (($env:Path -split ';') -notcontains $InstallDir) { $env:Path = "$env:Path;$InstallDir" }

    # The short alias (fm by default, CLI_ALIAS setting) next to the command
    & $Exe --no-update-check _install-alias-internal --bin-dir $InstallDir

    Write-Host ''
    Write-Host "$BinaryName v$Version installed to $Exe"
    Write-Host "Run '$BinaryName --version' to verify, then 'frankenmanager setup --install-mkcert' from an Administrator terminal on a first install."
}
