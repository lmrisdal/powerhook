# Install Powerhook on Windows: download the latest prebuilt release binary,
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
$RequireToken = $true # stays true when an existing config is kept

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
    Write-Host '==> Downloading Powerhook (amd64)'
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
        $answer = Read-Host 'Require a bearer token? [Y/n]'
        $RequireToken = $answer -notmatch '^\s*n'
        $Allow = Read-Host 'IP address(es) allowed to call the API (comma-separated, leave blank to skip IP restriction)'
        $ips = @($Allow -split '[,\s]+' | Where-Object { $_ })
        $token = ''
        if ($RequireToken) {
            $bytes = New-Object byte[] 32
            [Security.Cryptography.RandomNumberGenerator]::Create().GetBytes($bytes)
            $token = [Convert]::ToBase64String($bytes).TrimEnd('=').Replace('+', '-').Replace('/', '_')
        }
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
        if ($RequireToken) {
            Write-Host "    token: $token"
            Write-Host "    Clients send it as: Authorization: Bearer <token>"
        } else {
            Write-Host "    Token auth is off. To turn it on later, set `"token`" in $Conf"
            Write-Host "    to a random string of 16+ characters, then run: & '$Bin' reload"
        }
        $allowed = if ($ips.Count) { $ips -join ', ' } else { 'any' }
        Write-Host "    Allowed addresses: $allowed"
        Write-Host ''
        if (-not $RequireToken -and $ips.Count -eq 0) {
            $warn = @(
                '######################################################################'
                '#  WARNING: NO TOKEN AND NO IP RESTRICTION'
                '#'
                '#  Anyone who can reach port 9876 on this machine can suspend,'
                '#  power off or restart it. Only do this on a network you fully trust.'
                '#'
                "#  To lock it down later, edit $Conf, then run:"
                "#    & '$Bin' reload"
                '######################################################################'
            )
            $warn | ForEach-Object { Write-Host "    $_" -ForegroundColor Red }
            Write-Host ''
        }
    }

    Write-Host '==> Registering Windows service'
    $binPath = "`"$Bin`" --config `"$Conf`""
    if ($svc) {
        sc.exe config $Service binPath= $binPath start= auto | Out-Null
    } else {
        New-Service -Name $Service -BinaryPathName $binPath -DisplayName 'Powerhook' `
            -Description 'Remote suspend/poweroff/restart API' -StartupType Automatic | Out-Null
    }
    sc.exe failure $Service reset= 86400 actions= restart/5000/restart/5000/restart/30000 | Out-Null

    & $Bin --config $Conf status | Out-Null
    if ($LASTEXITCODE -ne 0) {
        Write-Host ''
        Write-Host 'The config is not valid yet (see the error above). Fix it, then finish with:'
        Write-Host "  notepad $Conf"
        Write-Host "  & '$Bin' reload"
        exit 1
    }

    # reload asks about the firewall itself, then starts the service.
    Write-Host '==> Configuring firewall and starting service'
    & $Bin --config $Conf reload
    if ($LASTEXITCODE -ne 0) { throw 'Applying the config failed.' }

    # The LAN address other machines will use: the IPv4 address of the adapter
    # with the default gateway. Falls back to the computer name.
    $HostAddr = $null
    try {
        $HostAddr = Get-NetIPConfiguration |
            Where-Object { $_.IPv4DefaultGateway -and $_.NetAdapter.Status -eq 'Up' } |
            ForEach-Object { $_.IPv4Address.IPAddress } |
            Select-Object -First 1
    } catch {}
    if (-not $HostAddr) { $HostAddr = $env:COMPUTERNAME }

    Write-Host ''
    Write-Host 'Done. Powerhook is listening; check it with:'
    Write-Host "  Get-Content -Wait $(Join-Path $ConfDir 'powerhook.log')"
    if ($RequireToken) {
        Write-Host "  curl.exe -H 'Authorization: Bearer <token>' http://${HostAddr}:9876/api/status"
    } else {
        Write-Host "  curl.exe http://${HostAddr}:9876/api/status"
    }
} finally {
    Remove-Item -Recurse -Force $Tmp -ErrorAction SilentlyContinue
}
