#!/usr/bin/env bash
# Install Powerhook on Linux: download the latest prebuilt release binary,
# write a config with a fresh token, install the systemd service and open the
# firewall (firewalld or ufw) for the allowed IPs.
#
#   curl -fsSL https://raw.githubusercontent.com/lmrisdal/powerhook/main/packaging/install.sh | bash
#
# Safe to re-run: it won't overwrite an existing config, and re-running updates
# the binary and restarts the service.
set -euo pipefail

REPO="${POWERHOOK_REPO:-lmrisdal/powerhook}"
BIN="/usr/local/bin/powerhook"
CONF_DIR="/etc/powerhook"
CONF="$CONF_DIR/config.json"
UNIT="powerhook.service"
SCRIPT_DIR=""
if [[ -n "${BASH_SOURCE[0]:-}" && -f "${BASH_SOURCE[0]}" ]]; then
  SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
fi

ASSUME_YES=0
OPEN_FIREWALL=1
REQUIRE_TOKEN=true # stays true with -y or when an existing config is kept

usage() {
  cat <<'USAGE'
Install Powerhook: binary, config, systemd service and firewall rules.

Options:
  -y, --yes      don't prompt
  --no-firewall  don't add firewall rules
  -h, --help     show this help
USAGE
}

while [[ $# -gt 0 ]]; do
  case "$1" in
    -y|--yes)  ASSUME_YES=1 ;;
    --no-firewall) OPEN_FIREWALL=0 ;;
    -h|--help) usage; exit 0 ;;
    *) echo "Unknown option: $1 (try --help)" >&2; exit 1 ;;
  esac
  shift
done

# ask <question> : echo the answer. Reads from the controlling terminal so it
# works under `curl | bash` (where stdin is the script). Empty with -y or when
# no terminal is available.
ask() {
  local q="$1" ans=""
  if [[ "$ASSUME_YES" -eq 1 ]]; then echo ""; return; fi
  if ! { exec 3<>/dev/tty; } 2>/dev/null; then echo ""; return; fi
  printf '%s ' "$q" >&3
  IFS= read -r ans <&3 || ans=""
  exec 3>&-
  echo "$ans"
}

case "$(uname -m)" in
  x86_64|amd64)  ARCH="amd64" ;;
  aarch64|arm64) ARCH="arm64" ;;
  *) echo "Unsupported architecture: $(uname -m)" >&2; exit 1 ;;
esac

TMP="$(mktemp -d)"
trap 'rm -rf "$TMP"' EXIT

have_gh() { command -v gh >/dev/null 2>&1 && gh auth status >/dev/null 2>&1; }

# fetch_repo_file <path-in-repo> <dest> : prefer a local copy, else pull from
# GitHub (via gh for private repos, else public raw).
fetch_repo_file() {
  local path="$1" dest="$2"
  if [[ -n "$SCRIPT_DIR" && -f "$SCRIPT_DIR/$path" ]]; then
    cp "$SCRIPT_DIR/$path" "$dest"
  elif have_gh; then
    gh api -H "Accept: application/vnd.github.raw" "repos/$REPO/contents/$path" > "$dest"
  else
    curl -fsSL -o "$dest" "https://raw.githubusercontent.com/$REPO/main/$path"
  fi
}

echo "==> Downloading Powerhook ($ARCH)"
asset="powerhook-linux-$ARCH"
if [[ -n "$SCRIPT_DIR" && -f "$SCRIPT_DIR/powerhook" ]]; then
  cp "$SCRIPT_DIR/powerhook" "$TMP/powerhook"
elif [[ -n "$SCRIPT_DIR" && -f "$SCRIPT_DIR/go.mod" ]] && command -v go >/dev/null 2>&1; then
  echo "    No local binary found; building from local source"
  (cd "$SCRIPT_DIR" && CGO_ENABLED=0 go build -o "$TMP/powerhook" .)
elif have_gh; then
  gh release download --repo "$REPO" --pattern "$asset" --output "$TMP/powerhook" --clobber
else
  url="https://github.com/$REPO/releases/latest/download/$asset"
  if ! curl -fSL -o "$TMP/powerhook" "$url"; then
    if command -v go >/dev/null 2>&1; then
      echo "    Release asset not found; building from source tarball"
      curl -fsSL -o "$TMP/source.tar.gz" "https://github.com/$REPO/archive/refs/heads/main.tar.gz"
      tar -xzf "$TMP/source.tar.gz" -C "$TMP"
      src="$(find "$TMP" -maxdepth 2 -type f -name go.mod -exec dirname {} \; | head -n 1)"
      if [[ -z "$src" ]]; then
        echo "Could not find Powerhook source in tarball" >&2
        exit 1
      fi
      (cd "$src" && CGO_ENABLED=0 go build -o "$TMP/powerhook" .)
    else
      echo "Download failed and Go is not installed for a source build." >&2
      echo "Install Go, or clone the repo and run: go build -o powerhook ." >&2
      exit 1
    fi
  fi
fi
chmod +x "$TMP/powerhook"

echo "==> Installing binary to $BIN"
sudo install -Dm755 "$TMP/powerhook" "$BIN.new"
sudo mv -f "$BIN.new" "$BIN"
# Fix the SELinux label on Fedora-based systems like Bazzite.
if command -v restorecon >/dev/null 2>&1; then sudo restorecon "$BIN" || true; fi

echo "==> Installing config to $CONF"
if sudo test -f "$CONF"; then
  echo "    $CONF already exists, leaving it untouched"
else
  ans="$(ask "Require a bearer token? [Y/n]:")"
  if [[ "$ans" =~ ^[[:space:]]*[nN] ]]; then REQUIRE_TOKEN=false; fi
  ALLOW="$(ask "IP address(es) allowed to call the API (comma-separated, leave blank to skip IP restriction):")"
  ips_json=""
  for ip in ${ALLOW//,/ }; do
    ips_json+="${ips_json:+, }\"$ip\""
  done
  token=""
  if [[ "$REQUIRE_TOKEN" == true ]]; then
    token="$(head -c 32 /dev/urandom | base64 | tr '+/' '-_' | tr -d '=\n')"
  fi
  fetch_repo_file "packaging/config.example.json" "$TMP/config.json"
  sed -i \
    -e "s|\"CHANGE_ME\"|\"$token\"|" \
    -e "s|\"allowed_ips\": \[[^]]*\]|\"allowed_ips\": [$ips_json]|" \
    "$TMP/config.json"
  sudo install -d -m700 "$CONF_DIR"
  sudo install -m600 "$TMP/config.json" "$CONF"
  allowed="${ips_json//\"/}"
  echo
  if [[ "$REQUIRE_TOKEN" == true ]]; then
    echo "    token: $token"
    echo "    Clients send it as: Authorization: Bearer <token>"
  else
    echo "    Token auth is off. To turn it on later, set \"token\" in $CONF"
    echo "    (e.g. to the output of: openssl rand -base64 32), then run: sudo powerhook reload"
  fi
  echo "    Allowed addresses: ${allowed:-any}"
  echo
  if [[ "$REQUIRE_TOKEN" == false && -z "$ips_json" ]]; then
    cat >&2 <<WARN
    ######################################################################
    #  WARNING: NO TOKEN AND NO IP RESTRICTION
    #
    #  Anyone who can reach port 9876 on this machine can suspend,
    #  power off or restart it. Only do this on a network you fully trust.
    #
    #  To lock it down later, edit $CONF, then run:
    #    sudo powerhook reload
    ######################################################################
WARN
    echo
  fi
fi

echo "==> Installing systemd unit"
fetch_repo_file "packaging/$UNIT" "$TMP/$UNIT"
sudo install -Dm644 "$TMP/$UNIT" "/etc/systemd/system/$UNIT"
sudo systemctl daemon-reload
sudo systemctl enable "$UNIT" >/dev/null 2>&1

if ! sudo "$BIN" --config "$CONF" status >/dev/null; then
  echo
  echo "The config isn't valid yet (see the error above). Fix it, then finish with:"
  echo "  sudo nano $CONF"
  echo "  sudo powerhook reload"
  exit 1
fi

# reload asks about the firewall itself, then starts the service.
reload_args=()
if [[ "$OPEN_FIREWALL" -eq 0 ]]; then
  reload_args+=(--no)
elif [[ "$ASSUME_YES" -eq 1 ]]; then
  reload_args+=(--yes)
fi
echo "==> Configuring firewall and starting service"
sudo "$BIN" --config "$CONF" reload ${reload_args[@]+"${reload_args[@]}"}

# The LAN address other machines will use: the source address of the default
# route. Falls back to the hostname if that can't be found.
HOST_ADDR="$(ip -4 route get 1.1.1.1 2>/dev/null | sed -n 's/.* src \([0-9.]*\).*/\1/p')"
HOST_ADDR="${HOST_ADDR:-$(hostname)}"

echo
echo "Done. Powerhook is listening; check it with:"
echo "  journalctl -u $UNIT -f"
if [[ "$REQUIRE_TOKEN" == true ]]; then
  echo "  curl -H 'Authorization: Bearer <token>' http://$HOST_ADDR:9876/api/status"
else
  echo "  curl http://$HOST_ADDR:9876/api/status"
fi
