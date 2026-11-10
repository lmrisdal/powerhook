# Powerhook

A small API that lets you **suspend**, **power off** and
**restart** a PC over HTTP, e.g. from Home Assistant.

- Bearer-token auth, IP allowlist, or both.
- IP allowlist (single addresses or CIDR ranges) (with optional firewall management)
- Runs as a systemd service (Linux) or a Windows service

## Install

### Linux

```sh
curl -fsSL https://raw.githubusercontent.com/lmrisdal/powerhook/main/packaging/install.sh | bash
```

The installer places:

- binary: `/usr/local/bin/powerhook`
- config: `/etc/powerhook/config.json`
- service: `/etc/systemd/system/powerhook.service` (runs as root, so suspend/poweroff/reboot need no polkit prompt)
- firewall (if you say yes when asked; skip with `--no-firewall`): a firewalld `powerhook` service + rich rules, or ufw rules commented `powerhook`

### Windows

From an **Administrator** PowerShell:

```powershell
irm https://raw.githubusercontent.com/lmrisdal/powerhook/main/packaging/install.ps1 | iex
```

The installer places:

- binary: `C:\Program Files\powerhook\powerhook.exe`
- config: `C:\ProgramData\powerhook\config.json` (folder restricted to SYSTEM and Administrators)
- service: `powerhook` (automatic start, LocalSystem, restarts on failure)
- firewall (if you say yes when asked): an inbound Windows Firewall rule `powerhook`, limited to the allowed IPs
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

Powerhook shows the installed components (service, firewall rules, binary,
config), then lets you remove some of them or everything.

## Usage

```text
powerhook [--config PATH] [command]

Commands:
  (none)           Run the API server. This is what the service runs.
  status           Validate the config and print a summary.
  reload [-y | -n]  Apply the config: validate it, check the firewall rule
                   for allowed_ips (firewalld, ufw or Windows Firewall) and
                   ask before changing it, then start or restart the
                   service. -y/--yes answers yes to every question,
                   -n/--no answers no. Needs sudo / Administrator.
  update [--force] Download and install the latest Powerhook release, then
                   restart the service. Needs sudo / Administrator.
  uninstall        Interactively remove Powerhook (needs sudo / Administrator).
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
  "allowed_ips": ["192.168.1.5"],
  "action_delay_ms": 1000,
  "dry_run": false,
  "force_close_apps": false,
  "commands": {}
}
```

| Key | Default | Meaning |
|---|---|---|
| `listen` | `":9876"` | Address and port to listen on |
| `token` | `""` (no token) | Shared secret, at least 16 characters. Generate with `openssl rand -base64 32`. Empty or missing turns the token check off |
| `allowed_ips` | `[]` (any address) | Addresses or CIDR ranges allowed to call the API. Empty or missing allows every address |
| `action_delay_ms` | `1000` | Delay between answering and acting |
| `dry_run` | `false` | Log actions instead of performing them, which is useful for testing |
| `force_close_apps` | `false` | Windows only: don't let apps block shutdown/restart (unsaved work is lost) |
| `commands` | `{}` | Named programs the API can run, see [Commands](#commands) |

Unknown keys are rejected, so a typo like `allowed_ip` fails loudly instead of
being ignored. After editing, apply the changes with:

```sh
sudo powerhook reload
```

On Windows, run `powerhook reload` from an Administrator terminal. It checks
the config first and leaves the running service alone if the config is invalid.
It then reads powerhook's current firewall rules, asks before changing them if
they don't match `allowed_ips` (`-y` answers yes to every question, `-n` answers
no), and starts or restarts the service. The service only reads the config at startup,
so changes (including a new token) take effect then. Without a terminal to ask
on, it updates powerhook's existing firewall rules and adds none.

## API

All endpoints need a source IP in `allowed_ips` (if set) and, if `token` is set, `Authorization: Bearer <token>`.

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

## Commands

Besides suspend, power off and restart, you can expose your own commands. They
are defined in the config and the API only ever runs them **by name**: nothing
from the request (no body, query string or header) reaches the command line.

```json
{
  "commands": {
    "backup": {
      "exec": ["/usr/local/bin/backup"],
      "timeout_s": 30
    }
  }
}
```

| Key | Default | Meaning |
|---|---|---|
| `exec` | required | Program and arguments. Run directly, not through a shell, and `exec[0]` must be an absolute path. For pipes or logic, point it at a script |
| `timeout_s` | `30` | The command is killed after this many seconds (max 3600) |

Names are 1-64 letters, digits, `-` or `_`.

| Method | Path | Response |
|--------|------|----------|
| GET    | `/api/commands` | `200 {"commands":["backup"]}` |
| POST   | `/api/commands/<name>` | `202 {"status":"accepted","action":"command:<name>","delay_ms":1000}`, or `404` for an unknown name |

```sh
curl -X POST -H "Authorization: Bearer $TOKEN" http://desktop:9876/api/commands/backup
```

Commands use the same IP/token checks and the same one-at-a-time rule as the
built-in actions (`409` while another is pending). The response comes first;
the command then runs after `action_delay_ms`, and its output and exit status
go to the log. With `dry_run` they are logged, not run. On Linux a command runs
as root with a minimal environment (`PATH` and `LANG` only); on Windows it runs
as SYSTEM and inherits the service's environment.

Treat this as remote code execution for whoever can pass the token and IP
checks: only list commands you're happy to have run, make the programs
writable by root only, and keep the token and `allowed_ips` tight.

## Build

```sh
make
make test
```

## Notes

- **Windows Fast Startup:** Powerhook's poweroff does a full shutdown, not a
  hybrid one, so NTFS partitions stay mountable from Linux. Shutting down from
  the Start menu may still use Fast Startup; disabling it is recommended on
  dual-boot machines.
- **Windows suspend** calls `SetSuspendState` directly (sleep, not hibernate).
  It doesn't work on Modern Standby (S0) machines, which are rare on desktops.
- **Plain HTTP** is intended for a trusted LAN. Anyone who can sniff that
  traffic can see the token; the IP allowlist and firewall rule limit who can
  use it.
- **Without a token** (`"token": ""`), anything that can send traffic
  from an allowed IP can call the API, including every user and program on that
  machine. Keep `allowed_ips` as narrow as possible, e.g. a single address.
  If you skip the token, also drop the `authorization` headers from the Home
  Assistant example.
- **No token and no `allowed_ips`** leaves the API open to anything that can
  reach the port: anyone on the network can suspend, power off or restart the
  machine. powerhook allows it but prints a warning from the installer,
  `powerhook status` and at startup. Only do this on a network you fully trust.
