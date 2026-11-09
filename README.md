# powerhook

A tiny HTTP API to **suspend**, **power off** and **restart** a PC remotely,
e.g. from Home Assistant.

- Bearer-token auth (constant-time compare), or IP allowlist only if you prefer
- IP allowlist (single addresses or CIDR ranges), checked before the token
- Runs as a systemd service (Linux) or a Windows service, so it works before anyone logs in
- Opens the firewall port only for the allowed IPs
- Answers `202` first, then acts after `action_delay_ms`, so the caller gets a clean response

## Install

### Linux

```sh
curl -fsSL https://raw.githubusercontent.com/lmrisdal/powerhook/main/packaging/install.sh | bash
```

The installer places:

- binary: `/usr/local/bin/powerhook`
- config: `/etc/powerhook/config.json`
- service: `/etc/systemd/system/powerhook.service` (runs as root, so suspend/poweroff/reboot need no polkit prompt)
- firewall: a firewalld `powerhook` service + rich rules, or ufw rules commented `powerhook`

### Windows

From an **Administrator** PowerShell:

```powershell
irm https://raw.githubusercontent.com/lmrisdal/powerhook/main/packaging/install.ps1 | iex
```

The installer places:

- binary: `C:\Program Files\powerhook\powerhook.exe`
- config: `C:\ProgramData\powerhook\config.json` (folder restricted to SYSTEM and Administrators)
- service: `powerhook` (automatic start, LocalSystem, restarts on failure)
- firewall: an inbound Windows Firewall rule `powerhook`, limited to the allowed IPs
- log: `C:\ProgramData\powerhook\powerhook.log`

## Update

```sh
sudo powerhook update
```

On Windows, run `powerhook update` from an Administrator terminal. Either way
it downloads the latest release, verifies its checksum and restarts the
service. Use `--force` to reinstall even when the version already matches.

## Uninstall

```sh
sudo powerhook uninstall
```

powerhook shows the installed components (service, firewall rules, binary,
config), then lets you remove some of them or everything.

## Usage

```text
powerhook [--config PATH] [command]

Commands:
  (none)           Run the API server. This is what the service runs.
  status           Validate the config and print a summary.
  firewall         Allow the API port from allowed_ips in firewalld, ufw or
                   Windows Firewall. Re-run after changing listen or
                   allowed_ips. Needs sudo / Administrator.
  token            Print a new random token.
  update [--force] Download and install the latest powerhook release, then
                   restart the service. Needs sudo / Administrator.
  uninstall        Interactively remove powerhook (needs sudo / Administrator).
  version          Print version.
```

Config is searched in this order when `--config` is omitted:

- Linux: `$POWERHOOK_CONFIG`, `/etc/powerhook/config.json`, `~/.config/powerhook/config.json`
- Windows: `$POWERHOOK_CONFIG`, `C:\ProgramData\powerhook\config.json`, `%APPDATA%\powerhook\config.json`

Logs on Linux: `journalctl -u powerhook.service -f`

## Configure

```json
{
  "listen": ":9876",
  "token": "…",
  "require_token": true,
  "allowed_ips": ["192.168.1.5"],
  "action_delay_ms": 1000,
  "dry_run": false,
  "force_close_apps": false
}
```

| Key | Default | Meaning |
|---|---|---|
| `listen` | `":9876"` | Address and port to listen on |
| `token` | required | Shared secret, at least 16 characters. Generate with `powerhook token` |
| `require_token` | `true` | Set to `false` to skip the token check and rely on `allowed_ips` alone. `token` can then be omitted, and `allowed_ips` may not contain `0.0.0.0/0` or `::/0` |
| `allowed_ips` | required | Addresses or CIDR ranges allowed to call the API. Use `"0.0.0.0/0"` to allow everyone |
| `action_delay_ms` | `1000` | Delay between answering and acting |
| `dry_run` | `false` | Log actions instead of performing them, which is useful for testing |
| `force_close_apps` | `false` | Windows only: don't let apps block shutdown/restart (unsaved work is lost) |

Unknown keys are rejected, so a typo like `allowed_ip` fails loudly instead of
being ignored. After editing:

```sh
sudo powerhook status
sudo powerhook firewall              # only if listen or allowed_ips changed
sudo systemctl restart powerhook
```

On Windows: `powerhook status`, `powerhook firewall`, `Restart-Service powerhook`.

## API

All endpoints need an allowed source IP and, unless `require_token` is `false`, `Authorization: Bearer <token>`.

| Method | Path            | Response |
|--------|-----------------|----------|
| POST   | `/api/suspend`  | `202 {"status":"accepted","action":"suspend","delay_ms":1000}` |
| POST   | `/api/poweroff` | `202 …` |
| POST   | `/api/restart`  | `202 …` |
| GET    | `/api/status`   | `200 {"hostname":"…","os":"linux","os_id":"cachyos","os_name":"CachyOS","uptime_seconds":1234,"version":"…","dry_run":false}` |

Errors: `403` IP not allowed · `401` bad/missing token · `409` an action is already pending · `405` wrong method.

```sh
curl -X POST -H "Authorization: Bearer $TOKEN" http://desktop:9876/api/suspend
```

`os_id` tells you which OS a dual-boot machine is running (`windows`, `cachyos`, `bazzite`, …).

## Home Assistant

`secrets.yaml`:

```yaml
powerhook_auth: "Bearer <your token>"
```

`configuration.yaml`:

```yaml
rest_command:
  desktop_suspend:
    url: http://192.168.1.50:9876/api/suspend
    method: post
    headers:
      authorization: !secret powerhook_auth
    timeout: 5
  desktop_poweroff:
    url: http://192.168.1.50:9876/api/poweroff
    method: post
    headers:
      authorization: !secret powerhook_auth
    timeout: 5
  desktop_restart:
    url: http://192.168.1.50:9876/api/restart
    method: post
    headers:
      authorization: !secret powerhook_auth
    timeout: 5

# Which OS is running (unavailable when the PC is off/asleep)
rest:
  - resource: http://192.168.1.50:9876/api/status
    headers:
      authorization: !secret powerhook_auth
    scan_interval: 30
    timeout: 3
    sensor:
      - name: Desktop OS
        value_template: "{{ value_json.os_id }}"

# Power switch: Wake-on-LAN to turn on, powerhook to turn off
switch:
  - platform: wake_on_lan
    name: Desktop
    mac: "AA:BB:CC:DD:EE:FF"
    host: 192.168.1.50
    turn_off:
      - action: rest_command.desktop_suspend
```

Wake-on-LAN works from suspend, and from power-off if it's enabled in the
BIOS/UEFI (and, on Windows, in the network adapter's power management
settings). Give the PC a DHCP reservation: on a dual-boot machine both OSes
use the same NIC, so they get the same IP.

## Build

```sh
make            # dist/powerhook-linux-{amd64,arm64} and dist/powerhook-windows-amd64.exe
make test
```

Cross-compiles from any OS; no cgo. Running `packaging/install.sh` from a
clone builds from local source if Go is installed.

## Notes

- **Windows Fast Startup:** powerhook's poweroff does a full shutdown, not a
  hybrid one, so NTFS partitions stay mountable from Linux. Shutting down from
  the Start menu may still use Fast Startup; disabling it is recommended on
  dual-boot machines.
- **Windows suspend** calls `SetSuspendState` directly (sleep, not hibernate).
  It doesn't work on Modern Standby (S0) machines, which are rare on desktops.
- **Plain HTTP** is intended for a trusted LAN. Anyone who can sniff that
  traffic can see the token; the IP allowlist and firewall rule limit who can
  use it.
- **Without a token** (`"require_token": false`), anything that can send traffic
  from an allowed IP can call the API, including every user and program on that
  machine. Keep `allowed_ips` as narrow as possible, e.g. a single address.
  If you skip the token, also drop the `authorization` headers from the Home
  Assistant example.
