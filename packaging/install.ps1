# Install powerhook on Windows: download the latest prebuilt release binary,
# write a config with a fresh token, register the Windows service and open
# Windows Firewall for the allowed IPs. Run from an Administrator PowerShell:
#
#   irm https://raw.githubusercontent.com/lmrisdal/powerhook/main/packaging/install.ps1 | iex
#
# Safe to re-run: it won't overwrite an existing config, and re-running updates
# the binary and restarts the service.
$ErrorActionPreference = 'Stop'
$ProgressPreference = 'SilentlyContinue' # makes Invoke-WebRequest much faster

$Repo = if ($env:POWERHOOK_REPO) { $env:POWERHOOK_REPO } else { 'lmrisdal/powerhook' }
$InstallDir = Join-Path $env:ProgramFiles 'powerhook'
$Bin = Join-Path $InstallDir 'powerhook.exe'
$ConfDir = Join-Path $env:ProgramData 'powerhook'
$Conf = Join-Path $ConfDir 'config.json'
$Service = 'powerhook'

$identity = [Security.Principal.WindowsPrincipal][Security.Principal.WindowsIdentity]::GetCurrent()
if (-not $identity.IsInRole([Security.Principal.WindowsBuiltInRole]::Administrator)) {
    throw 'Run this from an Administrator PowerShell.'
}
if ($env:PROCESSOR_ARCHITECTURE -ne 'AMD64') {
    throw "Unsupported architecture: $env:PROCESSOR_ARCHITECTURE (only amd64 builds are published)"
}

$Tmp = Join-Path ([IO.Path]::GetTempPath()) ("powerhook-" + [guid]::NewGuid())
New-Item -ItemType Directory -Path $Tmp | Out-Null
try {
    Write-Host '==> Downloading powerhook (amd64)'
    $exe = Join-Path $Tmp 'powerhook.exe'
    $local = if ($PSScriptRoot) { Join-Path $PSScriptRoot '..\powerhook.exe' } else { $null }
    if ($local -and (Test-Path $local)) {
        Copy-Item $local $exe
    } else {
        Invoke-WebRequest -UseBasicParsing -OutFile $exe `
            "https://github.com/$Repo/releases/latest/download/powerhook-windows-amd64.exe"
    }

    Write-Host "==> Installing binary to $Bin"
    $svc = Get-Service -Name $Service -ErrorAction SilentlyContinue
    if ($svc -and $svc.Status -ne 'Stopped') {
        Stop-Service -Name $Service
        $svc.WaitForStatus('Stopped', [TimeSpan]::FromSeconds(15))
    }
    New-Item -ItemType Directory -Force -Path $InstallDir | Out-Null
    Copy-Item $exe $Bin -Force

    Write-Host "==> Installing config to $Conf"
    New-Item -ItemType Directory -Force -Path $ConfDir | Out-Null
    # The config holds the token, and ProgramData is readable by all users by
    # default: restrict the folder to SYSTEM and Administrators.
    icacls $ConfDir /inheritance:r /grant:r '*S-1-5-18:(OI)(CI)F' '*S-1-5-32-544:(OI)(CI)F' | Out-Null
    if (Test-Path $Conf) {
        Write-Host "    $Conf already exists, leaving it untouched"
    } else {
        $Allow = Read-Host 'IP address(es) allowed to call the API (comma-separated)'
        $ips = @($Allow -split '[,\s]+' | Where-Object { $_ })
        $token = (& $Bin token).Trim()
        $cfg = [ordered]@{
            listen           = ':9876'
            token            = $token
            allowed_ips      = $ips
            action_delay_ms  = 1000
            dry_run          = $false
            force_close_apps = $false
        }
        # WriteAllText writes UTF-8 without a BOM.
        [IO.File]::WriteAllText($Conf, (ConvertTo-Json -InputObject $cfg))
        Write-Host ''
        Write-Host "    token: $token"
        Write-Host "    Clients send it as: Authorization: Bearer <token>"
        Write-Host ''
    }

    Write-Host '==> Registering Windows service'
    $binPath = "`"$Bin`" --config `"$Conf`""
    if ($svc) {
        sc.exe config $Service binPath= $binPath start= auto | Out-Null
    } else {
        New-Service -Name $Service -BinaryPathName $binPath -DisplayName 'powerhook' `
            -Description 'Remote suspend/poweroff/restart API' -StartupType Automatic | Out-Null
    }
    sc.exe failure $Service reset= 86400 actions= restart/5000/restart/5000/restart/30000 | Out-Null

    & $Bin --config $Conf status | Out-Null
    if ($LASTEXITCODE -ne 0) {
        Write-Host ''
        Write-Host 'The config is not valid yet (see the error above). Fix it, then finish with:'
        Write-Host "  notepad $Conf"
        Write-Host "  & '$Bin' firewall"
        Write-Host "  Start-Service $Service"
        exit 1
    }

    Write-Host '==> Opening Windows Firewall'
    & $Bin --config $Conf firewall
    if ($LASTEXITCODE -ne 0) { throw 'Adding the firewall rule failed.' }

    Write-Host '==> Starting service'
    Start-Service -Name $Service

    Write-Host ''
    Write-Host 'Done. powerhook is listening; check it with:'
    Write-Host "  Get-Content -Wait $(Join-Path $ConfDir 'powerhook.log')"
    Write-Host "  curl.exe -H 'Authorization: Bearer <token>' http://$($env:COMPUTERNAME):9876/api/status"
} finally {
    Remove-Item -Recurse -Force $Tmp -ErrorAction SilentlyContinue
}
