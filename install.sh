#!/usr/bin/env bash
set -Eeuo pipefail

REPO=https://github.com/mryuvraj1/ipv8
SERVER=${IPV8_SERVER:-}
TOKEN=
NAME=$(hostname 2>/dev/null || printf 'IPv8 installation')
INSTALL_PREFIX=${IPV8_INSTALL_PREFIX:-}
DRY_RUN=0
MENU=0
ARG_COUNT=$#
TTY_STATE=
TMP_DIR=
USE_SUDO=0
SYSTEM_CHANGES=0
TERMUX=0
HAD_CONFIG=0
WIDTH=56
CYAN= BLUE= GREEN= AMBER= RESET=
UI=/dev/stdout
if [[ -r /dev/tty && -w /dev/tty ]] && ( : 2>/dev/null >/dev/tty); then UI=/dev/tty; fi

usage() {
  cat <<'HELP'
Usage:
  bash <(curl -fsSL https://raw.githubusercontent.com/mryuvraj1/ipv8/main/install.sh)
  install.sh [--server HTTPS_ORIGIN] [--name NAME] [--prefix PATH]
  install.sh --dry-run [--server HTTPS_ORIGIN] [--name NAME] [--prefix PATH]

The interactive install reads a short-lived enrollment token without echo.
Linux binaries come from the project's checksum-manifested GitHub Releases.
If no compatible binary exists, an explicit Go source-build option is offered.
Termux on Android arm64 currently builds from source and runs without a service.
HELP
}

fail() { printf 'IPv8 installer: %s\n' "$*" >&2; exit 1; }
restore_tty() {
  if [[ -n $TTY_STATE ]]; then stty "$TTY_STATE" </dev/tty 2>/dev/null || :; TTY_STATE=; fi
}
cleanup() {
  restore_tty
  if [[ -n $TMP_DIR && -d $TMP_DIR ]]; then rm -rf -- "$TMP_DIR"; fi
  TOKEN=
}
on_signal() {
  restore_tty
  if (( SYSTEM_CHANGES )); then
    printf '\nIPv8 setup interrupted after installation began. Inspect the listed target paths and service state before retrying.\n' >"$UI" 2>/dev/null || :
  else
    printf '\nIPv8 setup cancelled. No system files or services were changed.\n' >"$UI" 2>/dev/null || :
  fi
  exit 130
}
trap cleanup EXIT
trap on_signal INT TERM
trap 'exit 129' HUP

while (($#)); do
  case "$1" in
    --server) (($# >= 2)) || fail 'missing --server value'; SERVER=$2; shift 2 ;;
    --name) (($# >= 2)) || fail 'missing --name value'; NAME=$2; shift 2 ;;
    --prefix) (($# >= 2)) || fail 'missing --prefix value'; INSTALL_PREFIX=$2; shift 2 ;;
    --dry-run) DRY_RUN=1; shift ;;
    --menu) MENU=1; shift ;;
    --interactive) shift ;;
    -h|--help) usage; exit 0 ;;
    *) printf 'Unknown option: %s\n' "$1" >&2; usage >&2; exit 2 ;;
  esac
done

detect_platform() {
  KERNEL=$(uname -s 2>/dev/null || printf unknown)
  MACHINE=$(uname -m 2>/dev/null || printf unknown)
  case "$MACHINE" in x86_64|amd64) ARCH=amd64 ;; aarch64|arm64) ARCH=arm64 ;; *) ARCH=unsupported ;; esac
  DISTRO=unknown
  PRETTY='Unknown operating system'
  GOOS=linux
  if [[ $KERNEL == Linux && ( ${PREFIX:-} == /data/data/com.termux/files/usr || ${PREFIX:-} == /data/user/0/com.termux/files/usr ) && -n ${TERMUX_VERSION:-} ]]; then
    TERMUX=1
    DISTRO=termux
    PRETTY='Termux on Android'
    GOOS=android
  elif [[ -r /etc/os-release ]]; then
    # shellcheck disable=SC1091
    . /etc/os-release
    DISTRO=${ID:-unknown}
    PRETTY=${PRETTY_NAME:-$DISTRO}
  fi
  if [[ -z $INSTALL_PREFIX ]]; then
    if (( TERMUX )); then INSTALL_PREFIX=${PREFIX}; else INSTALL_PREFIX=/usr/local; fi
  fi
}

detect_platform
set_paths() {
  BIN=$INSTALL_PREFIX/bin/ipv8
  if (( TERMUX )); then
    CONFIG=$PREFIX/etc/ipv8/agent.json
    UNIT=
  else
    CONFIG=/etc/ipv8/agent.json
    UNIT=/etc/systemd/system/ipv8-agent.service
  fi
  PANEL_ENV=/etc/ipv8/panel.env
  PANEL_UNIT=/etc/systemd/system/ipv8-panel.service
  PANEL_DATA=/var/lib/ipv8/ipv8.json
}
set_paths

if [[ -t 1 && ${TERM:-dumb} != dumb && -z ${NO_COLOR:-} ]] && command -v tput >/dev/null 2>&1; then
  colors=$(tput colors 2>/dev/null || printf 0)
  if [[ $colors =~ ^[0-9]+$ ]] && (( colors >= 8 )); then
    CYAN='\033[1;36m'; BLUE='\033[1;34m'; GREEN='\033[1;32m'; AMBER='\033[1;33m'; RESET='\033[0m'
  fi
  columns=$(tput cols 2>/dev/null || printf 56)
  [[ $columns =~ ^[0-9]+$ ]] || columns=56
  if (( columns < 30 )); then WIDTH=30; elif (( columns < 56 )); then WIDTH=$columns; fi
fi

out() { printf '%b' "$*" >"$UI"; }
rule() {
  local i
  for ((i=0; i<WIDTH; i++)); do printf '=' >>"$UI"; done
  printf '\n' >"$UI"
}
ask() {
  [[ $UI == /dev/tty ]] || fail 'interactive input requires a terminal; use --dry-run for host checks.'
  printf '%s' "$1" >"$UI"
  IFS= read -r REPLY </dev/tty || fail 'input ended; no installation changes were made.'
}
ask_default() {
  local prompt=$1 default=$2
  ask "$prompt [$default]: "
  REPLY=${REPLY:-$default}
}
ask_secret() {
  [[ $UI == /dev/tty && -r /dev/tty && -w /dev/tty ]] || fail 'an interactive terminal is required to enter the one-use token privately.'
  TTY_STATE=$(stty -g </dev/tty 2>/dev/null) || fail 'cannot safely disable terminal echo; install stty or use another terminal.'
  stty -echo </dev/tty || fail 'cannot disable terminal echo.'
  printf '%s' "$1" >"$UI"
  if ! IFS= read -r TOKEN </dev/tty; then restore_tty; fail 'could not read the one-use token.'; fi
  restore_tty
  printf '\n' >"$UI"
}
pause_menu() {
  printf '\nPress Enter to return to the menu.' >"$UI"
  IFS= read -r _ </dev/tty || :
}
valid_name() {
  [[ ${#1} -ge 1 && ${#1} -le 60 && $1 != *[![:print:]]* ]]
}
valid_prefix() {
  [[ $1 == /* && $1 != / && $1 != */ && $1 != *//* && $1 != */../* && $1 != */.. && $1 != *[!A-Za-z0-9_./-]* ]] || return 1
  if (( TERMUX )); then [[ $1 == "$PREFIX" || $1 == "$PREFIX"/* ]] || return 1; fi
  return 0
}
valid_server() {
  local rest host port local_only=0
  case "$1" in
    https://?*) rest=${1#https://} ;;
    http://localhost|http://localhost:*|http://127.0.0.1|http://127.0.0.1:*|http://\[::1\]|http://\[::1\]:*) rest=${1#http://}; local_only=1 ;;
    *) return 1 ;;
  esac
  [[ -n $rest && $rest != */* && $rest != *\?* && $rest != *\#* && $rest != *@* && $rest != *' '* && $rest != *'\\'* && $rest != *'"'* ]] || return 1
  host=$rest
  if [[ $rest == *:* ]]; then
    host=${rest%:*}; port=${rest##*:}
    [[ $port =~ ^[0-9]{1,5}$ ]] && (( 10#$port >= 1 && 10#$port <= 65535 )) || return 1
  fi
  [[ -n $host && $host != .* && $host != *..* && $host != *.-* && $host != -* && $host != *-. && $host != *- && $host != *. ]] || return 1
  [[ $host != *[!A-Za-z0-9.-]* ]] || return 1
  if (( local_only )); then [[ $host == localhost || $host == 127.0.0.1 || $host == '[::1]' ]] || return 1; fi
  return 0
}

menu_banner() {
  [[ $UI == /dev/tty ]] || fail 'interactive menu requires a terminal; run with --help for command options.'
  printf '\n' >"$UI"
  rule
  out "${CYAN}  ██████╗\n ██╔═══██╗\n ██║   ██║\n ╚██████╔╝\n ██╔═══██╗\n ██║   ██║\n ╚██████╔╝\n  ╚═════╝${RESET}\n"
  printf ' IPv8 NETWORK  |  Experimental Go Prototype\n' >"$UI"
  rule
  local installed_version='Not installed' panel_status='Not installed' agent_status='Not installed' service_status='Unavailable'
  if [[ -x $BIN ]]; then installed_version=$($BIN version 2>/dev/null | sed -n 's/^IPv8 experimental agent /IPv8 /p' | sed -n '1p'); [[ -n $installed_version ]] || installed_version='Unknown'; fi
  if [[ -f $PANEL_ENV && ! -e $PANEL_UNIT ]]; then panel_status='Unknown (panel config exists; service is unmanaged)'; fi
  if [[ -e $PANEL_UNIT ]] && ! has_panel; then panel_status='Unknown (service unit is not managed by IPv8)'; fi
  if [[ -f $CONFIG && ! -e $UNIT ]]; then agent_status='Unknown (agent config exists; service is unmanaged)'; fi
  if [[ -e $UNIT ]] && ! has_agent; then agent_status='Unknown (service unit is not managed by IPv8)'; fi
  if (( TERMUX )) && [[ -f $CONFIG ]]; then agent_status='Unknown (foreground process status is not tracked)'; fi
  if (( ! TERMUX )) && command -v systemctl >/dev/null 2>&1 && [[ -d /run/systemd/system ]]; then
    service_status='systemd available'
    if [[ -f $PANEL_UNIT ]] && grep -Fxq '# Managed by the IPv8 installer.' "$PANEL_UNIT"; then panel_status=$(systemctl is-active ipv8-panel.service 2>/dev/null || printf 'Inactive'); fi
    if [[ -f $UNIT ]] && grep -Fxq '# Managed by the IPv8 installer.' "$UNIT"; then agent_status=$(systemctl is-active ipv8-agent.service 2>/dev/null || printf 'Inactive'); fi
  elif (( TERMUX )); then service_status='Not available on Termux'; fi
  printf ' System  : %s / %s\n Panel   : %s\n Agent   : %s\n Service : %s\n Version : %s\n\n' "$PRETTY" "$MACHINE" "$panel_status" "$agent_status" "$service_status" "$installed_version" >"$UI"
}
show_information() {
  printf '\nIPv8 is experimental software. It creates local 256-bit identifiers and\nvalidates a local packet envelope. It does not provide Internet routing, a\nVPN, peer packet transport, or an IPv4/IPv6 replacement.\nThe optional agent sends authenticated status heartbeats to the panel.\n' >"$UI"
}
show_dns_guide() {
  printf '\nIPv8 DNS metadata (application-defined TXT convention)\n\nType:  TXT\nName:  _ipv8\nValue: v=ipv8-1; address=YOUR_CANONICAL_IPV8_ADDRESS\n       Optional: ; expires=YYYY-MM-DDTHH:MM:SSZ\n\nPublish this relative name in the DNS zone for your domain. A verifier queries\n_ipv8.<domain>. This is not a standard DNS record format or a new record type.\nDNS publishes metadata only; it does not create IPv8 transport or make the\ndestination reachable. Resolver caches may retain old answers until TTL expiry.\n' >"$UI"
}

check_prerequisites() {
  local errors=0 utility systemd_version
  detect_platform
  set_paths
  if (( TERMUX )); then
    if [[ $ARCH != arm64 ]]; then printf 'Unsupported Termux architecture: %s. Only Android arm64 is currently supported.\n' "$MACHINE" >"$UI"; errors=1; fi
    command -v go >/dev/null 2>&1 || { printf 'Termux source build requires Go 1.24 or newer; install Go with pkg install golang, then rerun. The installer does not install packages.\n' >"$UI"; errors=1; }
    if command -v go >/dev/null 2>&1; then check_go_version || { printf 'Go 1.24 or newer is required for the Termux source build.\n' >"$UI"; errors=1; }; fi
    printf 'Termux support: Android arm64, source build, user-local files, foreground agent (no service manager).\n' >"$UI"
  else
    [[ $KERNEL == Linux ]] || { printf 'Unsupported OS: %s (Linux required).\n' "$KERNEL" >"$UI"; errors=1; }
    case "$DISTRO" in debian|ubuntu|fedora|arch) ;; *) printf 'Unsupported distribution: %s. Supported: Debian, Ubuntu, Fedora, Arch Linux.\n' "$DISTRO" >"$UI"; errors=1 ;; esac
    [[ $ARCH != unsupported ]] || { printf 'Unsupported CPU architecture: %s. Supported: amd64, arm64.\n' "$MACHINE" >"$UI"; errors=1; }
    if [[ ! -d /run/systemd/system ]] || ! command -v systemctl >/dev/null 2>&1; then
      printf 'systemd is unavailable or not running.\n' >"$UI"; errors=1
    else
      systemd_version=$(systemctl --version 2>/dev/null | sed -n '1s/^systemd \([0-9][0-9]*\).*/\1/p')
      if [[ ! $systemd_version =~ ^[0-9]+$ ]] || (( systemd_version < 247 )); then
        printf 'systemd 247+ required; found %s.\n' "${systemd_version:-unknown}" >"$UI"; errors=1
      fi
    fi
  fi
  for utility in curl sha256sum grep sed install df awk mktemp stty tar cp cmp chmod mv stat; do
    command -v "$utility" >/dev/null 2>&1 || { printf 'Missing dependency: %s. Install it with your package manager, then rerun. IPv8 will not install packages automatically.\n' "$utility" >"$UI"; errors=1; }
  done
  if (( errors == 0 )); then printf 'Prerequisites are available for %s/%s.\n' "$GOOS" "$ARCH" >"$UI"; fi
  return "$errors"
}

check_go_version() {
  local version major minor
  version=$(go env GOVERSION 2>/dev/null) || return 1
  version=${version#go}
  major=${version%%.*}
  version=${version#*.}
  minor=${version%%.*}
  [[ $major =~ ^[0-9]+$ && $minor =~ ^[0-9]+$ ]] || return 1
  (( major > 1 || (major == 1 && minor >= 24) ))
}

check_disk_space() {
  local minimum=${1:-20480} disk_path=${2:-$INSTALL_PREFIX} available
  while [[ ! -d $disk_path ]]; do
    disk_path=${disk_path%/*}
    [[ -n $disk_path ]] || disk_path=/
    [[ $disk_path != "$INSTALL_PREFIX" ]] || break
  done
  available=$(df -Pk "$disk_path" 2>/dev/null | awk 'NR==2 {print $4}')
  [[ $available =~ ^[0-9]+$ ]] || fail "could not determine free disk space for $disk_path."
  (( available >= minimum )) || fail "at least $((minimum / 1024)) MiB free space is required under $disk_path."
}

read_config_server() {
  [[ -f $CONFIG && ! -L $CONFIG ]] || return 1
  sed -n 's/^[[:space:]]*"server":[[:space:]]*"\([^"]*\)".*/\1/p' "$CONFIG" | sed -n '1p'
}

verify_install_targets() {
  set_paths
  if (( TERMUX )); then
    [[ ! -L $PREFIX && -d $PREFIX ]] || fail 'Termux PREFIX is missing or is a symlink; refusing to install.'
    [[ ! -L $PREFIX/etc/ipv8 ]] || fail 'IPv8 config directory is a symlink; refusing to follow it.'
  else
    [[ ! -L /etc/ipv8 ]] || fail '/etc/ipv8 is a symlink; refusing to follow it.'
    [[ ! -e /etc/ipv8 || -d /etc/ipv8 ]] || fail '/etc/ipv8 exists and is not a directory.'
  fi
  if (( ! TERMUX && EUID != 0 && USE_SUDO )); then
    if as_root test -L "$CONFIG"; then fail "$CONFIG is a symlink; refusing to follow it."; fi
    if as_root test -e "$CONFIG"; then
      as_root test -f "$CONFIG" || fail "$CONFIG must be a regular file, not a directory."
      HAD_CONFIG=1
    else
      HAD_CONFIG=0
    fi
  elif [[ -e $CONFIG || -L $CONFIG ]]; then
    [[ ! -L $CONFIG && -f $CONFIG ]] || fail "$CONFIG must be a regular file, not a symlink or directory."
    HAD_CONFIG=1
  elif (( ! TERMUX )) && [[ -d /etc/ipv8 && ! -x /etc/ipv8 ]]; then
    HAD_CONFIG=1
  else
    HAD_CONFIG=0
  fi
  [[ ! -L $INSTALL_PREFIX && ( ! -e $INSTALL_PREFIX || -d $INSTALL_PREFIX ) ]] || fail "$INSTALL_PREFIX is not a safe directory."
  [[ ! -L $INSTALL_PREFIX/bin && ( ! -e $INSTALL_PREFIX/bin || -d $INSTALL_PREFIX/bin ) ]] || fail "$INSTALL_PREFIX/bin is not a safe directory."
  if [[ -L $BIN ]]; then
    fail "$BIN is a symlink; refusing to replace it."
  elif [[ -e $BIN ]]; then
    [[ -f $BIN ]] || fail "$BIN is not a regular file."
    "$BIN" version 2>/dev/null | grep -Fq 'IPv8 experimental agent' || fail "$BIN is not a recognized IPv8 binary; it was not changed."
    (( HAD_CONFIG )) || fail "$BIN exists without IPv8 enrollment config; refusing to replace it."
  fi
  if (( ! TERMUX )); then
    if [[ -L $UNIT ]]; then
      fail "$UNIT is a symlink; refusing to follow it."
    elif [[ -e $UNIT ]] && ! grep -Fxq '# Managed by the IPv8 installer.' "$UNIT"; then
      fail "$UNIT exists and is not managed by IPv8; it was not changed."
    fi
  fi
}

configure_session() {
  local existing_server
  while :; do
    ask_default 'Installation name' "$NAME"; NAME=$REPLY
    valid_name "$NAME" || { printf 'Use 1-60 printable characters for the installation name.\n' >"$UI"; continue; }
    ask_default 'Installation directory / binary prefix' "$INSTALL_PREFIX"; INSTALL_PREFIX=$REPLY
    valid_prefix "$INSTALL_PREFIX" || { printf 'Use an absolute path without spaces, .. segments, or symlinks. Termux paths must remain under PREFIX.\n' >"$UI"; continue; }
    set_paths
    if [[ -e $CONFIG || -L $CONFIG ]]; then
      [[ ! -L $CONFIG && -f $CONFIG ]] || fail "$CONFIG is not a regular file; refusing to overwrite it."
      HAD_CONFIG=1
      existing_server=$(read_config_server 2>/dev/null || :)
      if [[ -n $existing_server ]]; then SERVER=$existing_server; else SERVER='(retained from existing private agent config)'; fi
    elif (( ! TERMUX )) && [[ -d /etc/ipv8 && ! -x /etc/ipv8 ]]; then
      # A prior installer may have made the directory private (0700), hiding
      # the config from this unprivileged review. Recheck after sudo approval.
      HAD_CONFIG=1
      SERVER='(checked after privilege consent; existing config retained)'
    else
      HAD_CONFIG=0
      ask_default 'Public IPv8 panel URL (HTTPS)' "$SERVER"; SERVER=$REPLY
      valid_server "$SERVER" || { printf 'Use an HTTPS origin without path/query/credentials; HTTP is allowed only for localhost prototypes.\n' >"$UI"; SERVER=; continue; }
    fi
    break
  done
}

request_sudo() {
  (( TERMUX || EUID == 0 )) && return 0
  command -v sudo >/dev/null 2>&1 || fail 'Linux service installation needs root. Install sudo or restart this already-downloaded script as root after reviewing it.'
  ask 'Linux service installation requires root access for IPv8 paths and systemd only. Allow this installer to request sudo now? [y/N] '
  case "$REPLY" in y|Y|yes|YES) ;; *) fail 'root access was not approved; no system changes were made.' ;; esac
  sudo -v || fail 'sudo authorization failed; no system changes were made.'
  USE_SUDO=1
}

as_root() {
  if (( EUID == 0 )); then "$@"; else sudo "$@"; fi
}

http_download() {
  local url=$1 output=$2 code
  code=$(curl --proto '=https' --tlsv1.2 --connect-timeout 10 --max-time 180 --location --silent --show-error --output "$output" --write-out '%{http_code}' "$url") || fail "download request failed: $url"
  printf '%s' "$code"
}

build_from_source() {
  local archive=$TMP_DIR/ipv8-main.tar.gz entry
  if (( ! TERMUX && EUID == 0 )); then
    fail 'for safety, the unsigned source build must run as a normal user. Start this installer without sudo; it will request sudo only for the reviewed Linux file/service changes.'
  fi
  command -v go >/dev/null 2>&1 || fail 'no compatible release asset exists and Go 1.24+ is unavailable. Install Go yourself or wait for a published compatible release.'
  check_go_version || fail 'building IPv8 from source requires Go 1.24 or newer.'
  printf '\nNo compatible published binary was found. The official GitHub source snapshot has no signed checksum; it will be built as your user and will not be executed as root.\n' >"$UI"
  ask 'Confirm source build [y/N]: '
  case "$REPLY" in y|Y|yes|YES) ;; *) fail 'source build was declined; no system changes were made.' ;; esac
  check_disk_space 102400 "$TMP_DIR"
  printf '[1/6] Downloading the IPv8 source snapshot over HTTPS...\n'
  local code
  code=$(http_download "$REPO/archive/refs/heads/main.tar.gz" "$archive")
  [[ $code == 200 ]] || fail "GitHub returned HTTP $code for the IPv8 source snapshot. Retry later or inspect the repository manually."
  tar -tzf "$archive" >"$TMP_DIR/source-files.txt" || fail 'the downloaded source archive is malformed.'
  while IFS= read -r entry; do
    [[ $entry == ipv8-main/* && /$entry/ != */../* ]] || fail 'source archive contains an unexpected path; refusing extraction.'
  done <"$TMP_DIR/source-files.txt"
  mkdir -m 0700 "$TMP_DIR/source"
  tar -xzf "$archive" --strip-components=1 -C "$TMP_DIR/source" || fail 'could not safely extract the source archive.'
  [[ -f $TMP_DIR/source/go.mod && -f $TMP_DIR/source/cmd/ipv8/main.go ]] || fail 'source archive is missing required IPv8 project files.'
  printf '[2/6] Building for %s/%s with %s...\n' "$GOOS" "$ARCH" "$(go env GOVERSION)"
  (cd "$TMP_DIR/source" && CGO_ENABLED=0 GOOS="$GOOS" GOARCH="$ARCH" go build -trimpath -o "$TMP_DIR/ipv8" ./cmd/ipv8) || fail 'source build failed; check Go installation, memory, and network access for modules.'
  AGENT=$TMP_DIR/ipv8
  SERVICE_SOURCE=
  "$AGENT" version | grep -Fq 'IPv8 experimental agent' || fail 'the source-built agent did not pass its version check.'
  RELEASE_KIND='source build (HTTPS GitHub main snapshot; unsigned)'
}

download_agent() {
  local code checksum service_checksum checksum_count
  TMP_DIR=$(mktemp -d "${TMPDIR:-/tmp}/ipv8-install.XXXXXX") || fail 'could not create a private temporary directory.'
  chmod 0700 "$TMP_DIR"
  check_disk_space 20480 "$TMP_DIR"
  if (( TERMUX )); then
    build_from_source
    return
  fi
  ASSET=ipv8-linux-$ARCH
  SERVICE=ipv8-agent.service
  RELEASE="$REPO/releases/latest/download"
  printf '[1/6] Checking the published linux/%s release asset...\n' "$ARCH"
  code=$(http_download "$RELEASE/$ASSET" "$TMP_DIR/$ASSET")
  if [[ $code == 404 ]]; then
    build_from_source
    return
  fi
  [[ $code == 200 ]] || fail "GitHub returned HTTP $code while downloading the Linux agent; retry after checking GitHub Releases."
  code=$(http_download "$RELEASE/SHA256SUMS" "$TMP_DIR/SHA256SUMS")
  [[ $code == 200 ]] || fail "release checksum manifest returned HTTP $code; refusing an unverified binary."
  code=$(http_download "$RELEASE/$SERVICE" "$TMP_DIR/$SERVICE")
  [[ $code == 200 ]] || fail "release service unit returned HTTP $code; refusing an incomplete release."
  checksum_count=$(awk -v f="$ASSET" '$2==f {n++} END {print n+0}' "$TMP_DIR/SHA256SUMS")
  service_checksum_count=$(awk -v f="$SERVICE" '$2==f {n++} END {print n+0}' "$TMP_DIR/SHA256SUMS")
  [[ $checksum_count == 1 && $service_checksum_count == 1 ]] || fail 'release checksum manifest must contain exactly one entry per required asset.'
  checksum=$(awk -v f="$ASSET" '$2==f {print $1}' "$TMP_DIR/SHA256SUMS")
  service_checksum=$(awk -v f="$SERVICE" '$2==f {print $1}' "$TMP_DIR/SHA256SUMS")
  [[ $checksum =~ ^[[:xdigit:]]{64}$ && $service_checksum =~ ^[[:xdigit:]]{64}$ ]] || fail 'release checksum manifest contains an invalid digest.'
  printf '[2/6] Verifying release checksums...\n'
  (cd "$TMP_DIR" && printf '%s  %s\n%s  %s\n' "$checksum" "$ASSET" "$service_checksum" "$SERVICE" | sha256sum -c - >/dev/null) || fail 'release checksum verification failed.'
  chmod 0755 "$TMP_DIR/$ASSET"
  AGENT=$TMP_DIR/$ASSET
  SERVICE_SOURCE=$TMP_DIR/$SERVICE
  "$AGENT" version 2>/dev/null | grep -Fq 'IPv8 experimental agent' || fail 'downloaded binary did not pass its version check.'
  RELEASE_KIND="published linux/$ARCH release (SHA256SUMS verified; checksum manifest is not a signature)"
}

install_flow() {
  while :; do
    configure_session
    detect_platform
    ask 'Accept that IPv8 is experimental software with no packet transport or public routing? [y/N] '
    case "$REPLY" in y|Y|yes|YES) ;; *) printf 'Terms were not accepted. No system files were changed.\n' >"$UI"; return 0 ;; esac
    if (( HAD_CONFIG )); then
      printf '\nExisting enrollment credential will be retained; the installer will not request a new token or replace the config.\n' >"$UI"
      TOKEN=
      ENROLL_SUMMARY='Existing credential retained'
    else
      ask_secret 'One-use enrollment token (input hidden): '
      [[ ${#TOKEN} -ge 16 && ${#TOKEN} -le 128 ]] || { TOKEN=; fail 'the enrollment token must be 16-128 characters.'; }
      [[ $TOKEN != *[!A-Za-z0-9_-]* ]] || { TOKEN=; fail 'the enrollment token contains unsupported characters.'; }
      ENROLL_SUMMARY='One-use token; memory only; expires after 15 minutes'
    fi

    printf '\nIPv8 installation review\n' >"$UI"
    rule
    if (( TERMUX )); then
      printf ' Installation directory : %s\n Configuration          : %s (0600; enrollment credential)\n Detected platform       : %s / android/%s\n Agent source            : Go 1.24+ build required; no Android release is currently published\n Panel URL               : %s\n Installation name       : %s\n Enrollment              : %s\n Data sent to panel      : name, OS/architecture, agent version, heartbeats\n DNS integration         : disabled in agent; TXT metadata is configured separately\n Privileges / files      : current Termux user; %s, %s\n Service                 : none; run the agent in a Termux session\n Network                 : HTTPS to GitHub and configured panel\n System changes          : Termux PREFIX files only; no Android routes/firewall/DNS edits\n' "$INSTALL_PREFIX/bin" "$CONFIG" "$PRETTY" "$ARCH" "$SERVER" "$NAME" "$ENROLL_SUMMARY" "$BIN" "$CONFIG" >"$UI"
    else
      printf ' Installation directory : %s/bin\n Configuration          : %s (0600; panel and agent credential)\n Detected platform       : %s / linux/%s\n Agent source            : published checksum-verified release; Go source build offered if no matching release\n Panel URL               : %s\n Installation name       : %s\n Enrollment              : %s\n Data sent to panel      : name, OS/architecture, agent version, heartbeats\n DNS integration         : disabled in agent; TXT metadata is configured separately\n Optional components     : none\n Privileges / files      : root access; sudo consent when run as a normal user\n                         %s, %s, %s\n Service                 : systemd ipv8-agent.service (existing managed unit backed up if changed)\n Network                 : HTTPS to GitHub and configured panel\n System changes          : install/update IPv8 files and start service; no route, firewall, or DNS edits\n' "$INSTALL_PREFIX/bin" "$CONFIG" "$PRETTY" "$ARCH" "$SERVER" "$NAME" "$ENROLL_SUMMARY" "$BIN" "$CONFIG" "$UNIT" >"$UI"
    fi
    printf '\n[1] Confirm and start installation\n[2] Go back and edit configuration\n[3] Cancel installation\n' >"$UI"
    ask 'Choose an option [1-3]: '
    case "$REPLY" in
      1) break ;;
      2) (( HAD_CONFIG )) || TOKEN=; continue ;;
      3) TOKEN=; printf 'Installation cancelled; no download or system changes made.\n' >"$UI"; return 0 ;;
      *) printf 'Choose 1, 2, or 3.\n' >"$UI" ;;
    esac
  done

  check_prerequisites || fail 'prerequisite checks failed; install the named dependencies yourself, then rerun.'
  check_disk_space 20480
  if (( ! TERMUX )); then request_sudo; fi
  verify_install_targets
  if (( ! HAD_CONFIG )) && [[ -z $TOKEN ]]; then
    fail 'no existing config was found in the protected directory, but this review did not collect a panel URL or token. Restart the installer to review a new enrollment before continuing.'
  fi
  if (( HAD_CONFIG )); then
    if (( TERMUX )); then config_mode=$(stat -c '%a' "$CONFIG") || fail 'could not inspect the existing agent config permissions.'
    else config_mode=$(as_root stat -c '%a' "$CONFIG") || fail 'could not inspect the existing agent config permissions.'; fi
    [[ $config_mode =~ ^[0-7]{3,4}$ ]] && (( (8#$config_mode & 077) == 0 )) || fail "$CONFIG must be mode 0600 or stricter; it was not changed."
  fi

  download_agent
  if (( ! TERMUX )); then
    check_disk_space 20480
    if [[ -n ${SERVICE_SOURCE:-} ]]; then
      [[ -f $SERVICE_SOURCE ]] || fail 'the release did not provide a systemd unit.'
      sed "s|^ExecStart=/usr/local/bin/ipv8 |ExecStart=$BIN |" "$SERVICE_SOURCE" >"$TMP_DIR/service.custom"
    else
      cat >"$TMP_DIR/service.source" <<'UNIT'
# Managed by the IPv8 installer.
[Unit]
Description=IPv8 experimental heartbeat agent
After=network-online.target
Wants=network-online.target

[Service]
Type=simple
DynamicUser=yes
LoadCredential=agent.json:/etc/ipv8/agent.json
ExecStart=/usr/local/bin/ipv8 agent --config %d/agent.json
Restart=on-failure
RestartSec=5
NoNewPrivileges=true
ProtectSystem=strict
ProtectHome=true
PrivateTmp=true
PrivateDevices=true
ProtectKernelTunables=true
ProtectKernelModules=true
ProtectControlGroups=true
CapabilityBoundingSet=
RestrictAddressFamilies=AF_UNIX AF_INET AF_INET6
LockPersonality=true

[Install]
WantedBy=multi-user.target
UNIT
      sed "s|^ExecStart=/usr/local/bin/ipv8 |ExecStart=$BIN |" "$TMP_DIR/service.source" >"$TMP_DIR/service.custom"
    fi
    grep -Fq "ExecStart=$BIN agent" "$TMP_DIR/service.custom" || fail 'could not configure the service for the selected binary path.'
  else
    check_disk_space 102400
  fi
  printf 'Agent source: %s\n' "$RELEASE_KIND"
  if (( ! HAD_CONFIG )) && ! curl --proto '=http,https' --proto-redir '=https' --fail --silent --show-error --location --max-time 15 --output /dev/null "$SERVER/healthz"; then
    fail 'configured panel health endpoint is unreachable; check URL, DNS, tunnel, and outbound connectivity. No new agent credential was enrolled.'
  fi

  if (( HAD_CONFIG )); then
    printf '[3/6] Retaining the existing private enrollment config; the agent validates it when systemd starts...\n'
  else
    printf '[3/6] Enrolling node with the panel over HTTPS...\n'
    SYSTEM_CHANGES=1
    if (( TERMUX )); then
      ENROLL_CONFIG=$CONFIG
    else
      as_root install -d -m 0700 /etc/ipv8
      as_root test ! -e "$CONFIG" && as_root test ! -L "$CONFIG" || fail "$CONFIG appeared during setup; refusing to overwrite it."
      ENROLL_CONFIG=$TMP_DIR/agent.json
    fi
    printf '%s' "$TOKEN" | "$AGENT" enroll --server "$SERVER" --name "$NAME" --token-stdin --config "$ENROLL_CONFIG" || fail 'enrollment failed; request a fresh token if it expired or was consumed.'
    TOKEN=
    if (( ! TERMUX )); then
      as_root test ! -e "$CONFIG" && as_root test ! -L "$CONFIG" || fail "$CONFIG appeared during enrollment; the installer will not overwrite it. A fresh token may be needed."
      as_root install -m 0600 "$ENROLL_CONFIG" "$CONFIG" || fail "could not save the enrolled credential securely at $CONFIG; check permissions and request a fresh token if needed."
    fi
  fi

  printf '[4/6] Installing the agent binary...\n'
  SYSTEM_CHANGES=1
  if (( TERMUX )); then
    install -d -m 0755 "$INSTALL_PREFIX/bin"
    install -m 0755 "$AGENT" "$TMP_DIR/ipv8.new"
    mv -f "$TMP_DIR/ipv8.new" "$BIN"
  else
    as_root install -d -m 0755 "$INSTALL_PREFIX/bin"
    install -m 0755 "$AGENT" "$TMP_DIR/ipv8.new"
    as_root install -m 0755 "$TMP_DIR/ipv8.new" "$BIN"
  fi
  "$BIN" version >/dev/null || fail 'installed binary failed its version check.'

  if (( TERMUX )); then
    printf '[5/6] No service manager is available in Termux; no background service was created.\n'
    printf '[6/6] Installation files are ready. Start the agent in a Termux session with:\n  %s agent --config %s\nThe dashboard will show Online only after the panel receives an authenticated heartbeat.\n' "$BIN" "$CONFIG"
    return 0
  fi

  printf '[5/6] Installing and starting the systemd service...\n'
  as_root install -d -m 0755 /etc/systemd/system
  if as_root test -e "$UNIT" && ! cmp -s "$TMP_DIR/service.custom" "$UNIT"; then
    local backup="$UNIT.ipv8-backup.$$" suffix=0
    while as_root test -e "$backup" || as_root test -L "$backup"; do
      suffix=$((suffix + 1))
      backup="$UNIT.ipv8-backup.$$.$suffix"
    done
    as_root cp -p "$UNIT" "$backup" || fail "could not back up the existing managed unit at $UNIT; it was not overwritten."
    printf 'Previous IPv8-managed service unit backed up to %s\n' "$backup" >"$UI"
  fi
  as_root install -m 0644 "$TMP_DIR/service.custom" "$UNIT"
  as_root systemctl daemon-reload
  as_root systemctl enable ipv8-agent.service >/dev/null
  if as_root systemctl is-active --quiet ipv8-agent.service; then as_root systemctl restart ipv8-agent.service; else as_root systemctl start ipv8-agent.service; fi
  printf '[6/6] Checking systemd service startup...\n'
  as_root systemctl is-active --quiet ipv8-agent.service || fail 'service did not start; inspect with: sudo journalctl -u ipv8-agent -n 50'
  printf 'IPv8 agent service is active. The panel marks the node Online only after an authenticated heartbeat arrives.\n'
  printf '\nAfter revoking the node in My IPv8, run:\n  bash <(curl -fsSL https://raw.githubusercontent.com/mryuvraj1/ipv8/main/install.sh)\nThen choose [4] Uninstall IPv8 Agent. The manager preserves the local enrollment config by default.\n' >"$UI"
}

configure_menu() {
  ask_default 'Panel URL (HTTPS)' "$SERVER"; SERVER=$REPLY
  while ! valid_server "$SERVER"; do printf 'Invalid origin. HTTPS is required except for localhost.\n' >"$UI"; ask_default 'Panel URL (HTTPS)' ''; SERVER=$REPLY; done
  ask_default 'Installation name' "$NAME"; NAME=$REPLY
  while ! valid_name "$NAME"; do printf 'Use 1-60 printable characters.\n' >"$UI"; ask_default 'Installation name' "$(hostname 2>/dev/null || printf 'IPv8 installation')"; NAME=$REPLY; done
  ask_default 'Installation directory / binary prefix' "$INSTALL_PREFIX"; INSTALL_PREFIX=$REPLY
  while ! valid_prefix "$INSTALL_PREFIX"; do printf 'Use a safe absolute path; Termux paths must remain under PREFIX.\n' >"$UI"; ask_default 'Installation directory / binary prefix' "$PREFIX"; INSTALL_PREFIX=$REPLY; done
  set_paths
  printf '\nConfiguration for this menu session:\n  Panel: %s\n  Name: %s\n  Prefix: %s\n  Enrollment token: requested privately only when Install is selected.\n  DNS: metadata is configured separately; agent does not use DNS.\n' "$SERVER" "$NAME" "$INSTALL_PREFIX" >"$UI"
}

configure_panel_menu() {
  if ! has_panel; then
    printf 'No IPv8-managed panel configuration exists. Agent settings below apply only to this installer session; install a panel to configure persistent panel settings.\n' >"$UI"
    configure_menu
    return
  fi
  IPV8_ADDR=$(env_value "$PANEL_ENV" IPV8_ADDR 2>/dev/null || printf '127.0.0.1:8080')
  IPV8_DATA=$(env_value "$PANEL_ENV" IPV8_DATA 2>/dev/null || printf '%s' "$PANEL_DATA")
  IPV8_PUBLIC_URL=$(env_value "$PANEL_ENV" IPV8_PUBLIC_URL 2>/dev/null || :)
  IPV8_API_URL=$(env_value "$PANEL_ENV" IPV8_API_URL 2>/dev/null || :)
  IPV8_COOKIE_SECURE=$(env_value "$PANEL_ENV" IPV8_COOKIE_SECURE 2>/dev/null || printf false)
  while ! panel_configure_values; do :; done
  printf '\nConfiguration review\n Listen address: %s (loopback only)\n Data file: %s (existing data preserved)\n Public URL: %s\n API URL: %s\n Secure cookies: %s\n' "$IPV8_ADDR" "$IPV8_DATA" "${IPV8_PUBLIC_URL:-local-only}" "${IPV8_API_URL:-same-origin local}" "$IPV8_COOKIE_SECURE" >"$UI"
  ask 'Save a backup, update the managed panel configuration, and restart the panel? [y/N] '
  case "$REPLY" in y|Y|yes|YES) ;; *) printf 'Configuration edit cancelled.\n' >"$UI"; return 0 ;; esac
  request_sudo
  as_root test ! -L "$PANEL_ENV" && as_root test -f "$PANEL_ENV" || fail 'managed panel environment file is missing or unsafe.'
  backup_file "$PANEL_ENV"
  TMP_DIR=$(mktemp -d "${TMPDIR:-/tmp}/ipv8-config.XXXXXX") || fail 'could not create a private temporary directory.'; chmod 0700 "$TMP_DIR"
  cat >"$TMP_DIR/panel.env" <<EOF
# Managed by the IPv8 installer.
IPV8_ADDR=$IPV8_ADDR
IPV8_DATA=$IPV8_DATA
IPV8_PUBLIC_URL=$IPV8_PUBLIC_URL
IPV8_API_URL=$IPV8_API_URL
IPV8_COOKIE_SECURE=$IPV8_COOKIE_SECURE
EOF
  as_root install -m 0600 "$TMP_DIR/panel.env" "$PANEL_ENV"
  if ! as_root systemctl restart ipv8-panel.service; then
    as_root install -m 0600 "$BACKUP_PATH" "$PANEL_ENV"
    as_root systemctl restart ipv8-panel.service || :
    fail 'panel restart failed; previous configuration was restored from the backup.'
  fi
  if curl --proto '=http,https' --proto-redir '=https' --fail --silent --max-time 5 "http://$IPV8_ADDR/healthz" >/dev/null 2>&1; then printf 'Configuration saved and local panel health returned HTTP 200.\n' >"$UI"; else printf 'Configuration saved, but health is unavailable. Inspect: sudo journalctl -u ipv8-panel -n 50 --no-pager\n' >"$UI"; fi
}

valid_panel_addr() {
  local host port
  [[ $1 == *:* ]] || return 1
  host=${1%:*}; port=${1##*:}
  [[ $port =~ ^[0-9]{1,5}$ ]] && (( 10#$port > 0 && 10#$port <= 65535 )) || return 1
  [[ $host == 127.0.0.1 || $host == localhost || $host == '[::1]' ]]
}
valid_data_path() {
  local file=${1#/var/lib/ipv8/}
  [[ $1 == /var/lib/ipv8/* && -n $file && $file != */* && $file != . && $file != .. && $file != *[!A-Za-z0-9_.-]* ]]
}
env_value() {
  [[ -f $1 && ! -L $1 ]] || return 1
  awk -F= -v key="$2" '$1==key {sub(/^[^=]*=/, ""); print; exit}' "$1"
}
has_agent() { [[ -f $UNIT && ! -L $UNIT ]] && grep -Fxq '# Managed by the IPv8 installer.' "$UNIT"; }
has_panel() { [[ -f $PANEL_UNIT && ! -L $PANEL_UNIT ]] && grep -Fxq '# Managed by the IPv8 installer.' "$PANEL_UNIT"; }
is_ipv8_binary() { [[ -f $BIN && -x $BIN && ! -L $BIN ]] && "$BIN" version 2>/dev/null | grep -Fq 'IPv8 experimental agent'; }
remove_unused_binary() {
  if ! has_agent && ! has_panel && is_ipv8_binary; then as_root rm -f -- "$BIN"; fi
}
require_systemd() {
  if (( TERMUX )) || [[ $KERNEL != Linux || ! -d /run/systemd/system ]] || ! command -v systemctl >/dev/null 2>&1; then
    printf 'This action requires Linux with a running systemd service manager. Termux service management is unavailable.\n' >"$UI"
    return 1
  fi
}
backup_file() {
  local path=$1 backup="$1.ipv8-backup.$(date +%Y%m%d%H%M%S).$$" suffix=0
  while as_root test -e "$backup" || as_root test -L "$backup"; do suffix=$((suffix + 1)); backup="$1.ipv8-backup.$(date +%Y%m%d%H%M%S).$$.$suffix"; done
  as_root cp -p -- "$path" "$backup" || fail "could not back up $path; it was not replaced."
  BACKUP_PATH=$backup
  printf ' Backup: %s\n' "$backup" >"$UI"
}

panel_configure_values() {
  local old_addr=$IPV8_ADDR old_data=$IPV8_DATA old_public=$IPV8_PUBLIC_URL old_api=$IPV8_API_URL old_secure=$IPV8_COOKIE_SECURE
  ask_default 'Listen address (loopback only)' "$IPV8_ADDR"; IPV8_ADDR=$REPLY
  valid_panel_addr "$IPV8_ADDR" || { printf 'Use 127.0.0.1:8080, localhost:8080, or [::1]:8080. Public HTTP listeners are disabled.\n' >"$UI"; IPV8_ADDR=$old_addr; return 1; }
  ask_default 'Persistent database file' "$IPV8_DATA"; IPV8_DATA=$REPLY
  valid_data_path "$IPV8_DATA" || { printf 'Use a safe file path below /var/lib/ipv8/.\n' >"$UI"; IPV8_ADDR=$old_addr; IPV8_DATA=$old_data; return 1; }
  ask_default 'Public panel URL (blank for local-only)' "$IPV8_PUBLIC_URL"; IPV8_PUBLIC_URL=$REPLY
  if [[ -n $IPV8_PUBLIC_URL ]] && { [[ $IPV8_PUBLIC_URL != https://* ]] || ! valid_server "$IPV8_PUBLIC_URL"; }; then
    printf 'Public URL must be a valid HTTPS origin with no path.\n' >"$UI"; IPV8_ADDR=$old_addr; IPV8_DATA=$old_data; IPV8_PUBLIC_URL=$old_public; return 1
  fi
  ask_default 'API URL (must equal public URL)' "$IPV8_API_URL"; IPV8_API_URL=$REPLY
  if [[ -n $IPV8_PUBLIC_URL || -n $IPV8_API_URL ]]; then
    [[ -n $IPV8_PUBLIC_URL && $IPV8_PUBLIC_URL == "$IPV8_API_URL" ]] || { printf 'The panel requires the same HTTPS origin for public and API URLs.\n' >"$UI"; IPV8_ADDR=$old_addr; IPV8_DATA=$old_data; IPV8_PUBLIC_URL=$old_public; IPV8_API_URL=$old_api; return 1; }
    IPV8_COOKIE_SECURE=true
  else
    ask_default 'Secure cookies (true/false)' "$IPV8_COOKIE_SECURE"; IPV8_COOKIE_SECURE=$REPLY
  fi
  [[ $IPV8_COOKIE_SECURE == true || $IPV8_COOKIE_SECURE == false ]] || { printf 'Set secure cookies to true or false.\n' >"$UI"; IPV8_ADDR=$old_addr; IPV8_DATA=$old_data; IPV8_PUBLIC_URL=$old_public; IPV8_API_URL=$old_api; IPV8_COOKIE_SECURE=$old_secure; return 1; }
  [[ -z $IPV8_PUBLIC_URL || $IPV8_COOKIE_SECURE == true ]] || { printf 'Secure cookies must be true when a public HTTPS URL is configured.\n' >"$UI"; IPV8_COOKIE_SECURE=$old_secure; return 1; }
}

install_panel_flow() {
  (( TERMUX )) && { printf 'Panel systemd installation is not supported on Termux. Use the documented local Go run command there.\n' >"$UI"; return 0; }
  require_systemd || return 0
  valid_prefix "$INSTALL_PREFIX" || { printf 'IPV8_INSTALL_PREFIX is not a safe absolute path.\n' >"$UI"; return 0; }
  IPV8_ADDR=$(env_value "$PANEL_ENV" IPV8_ADDR 2>/dev/null || printf '127.0.0.1:8080')
  IPV8_DATA=$(env_value "$PANEL_ENV" IPV8_DATA 2>/dev/null || printf '%s' "$PANEL_DATA")
  IPV8_PUBLIC_URL=$(env_value "$PANEL_ENV" IPV8_PUBLIC_URL 2>/dev/null || :)
  IPV8_API_URL=$(env_value "$PANEL_ENV" IPV8_API_URL 2>/dev/null || :)
  IPV8_COOKIE_SECURE=$(env_value "$PANEL_ENV" IPV8_COOKIE_SECURE 2>/dev/null || printf false)
  while ! panel_configure_values; do :; done
  ask 'Accept that IPv8 is experimental and this panel does not route packets? [y/N] '
  case "$REPLY" in y|Y|yes|YES) ;; *) printf 'Terms were not accepted; no files or services were changed.\n' >"$UI"; return 0 ;; esac
  printf '\nIPv8 panel installation review\n'; rule
  printf ' Platform       : %s / linux/%s\n Listen address : %s (loopback only)\n Database       : %s (preserved)\n Public URL     : %s\n API URL        : %s\n Secure cookies : %s\n Binary         : %s\n Service        : ipv8-panel.service (systemd DynamicUser)\n Privileges     : root for installation and systemd; requested after confirmation\n Files changed  : %s, %s, %s\n' "$PRETTY" "$ARCH" "$IPV8_ADDR" "$IPV8_DATA" "${IPV8_PUBLIC_URL:-local-only}" "${IPV8_API_URL:-same-origin local}" "$IPV8_COOKIE_SECURE" "$BIN" "$BIN" "$PANEL_ENV" "$PANEL_UNIT" >"$UI"
  printf '\n[1] Confirm and install\n[2] Edit configuration\n[3] Cancel\n' >"$UI"
  ask 'Choose an option [1-3]: '
  case "$REPLY" in 1) ;; 2) install_panel_flow; return $? ;; 3) printf 'Installation cancelled; no download or changes made.\n' >"$UI"; return 0 ;; *) printf 'Invalid choice; no changes made.\n' >"$UI"; return 0 ;; esac
  check_prerequisites || fail 'prerequisite checks failed; no files were changed.'
  check_disk_space 20480
  [[ ! -L /etc/ipv8 && ( ! -e /etc/ipv8 || -d /etc/ipv8 ) ]] || fail '/etc/ipv8 is not a safe directory.'
  [[ ! -L $PANEL_ENV && ( ! -e $PANEL_ENV || -f $PANEL_ENV ) ]] || fail "$PANEL_ENV is not a regular file."
  [[ ! -L $PANEL_UNIT && ( ! -e $PANEL_UNIT || -f $PANEL_UNIT ) ]] || fail "$PANEL_UNIT is not a regular file."
  [[ ! -e $PANEL_ENV ]] || grep -Fxq '# Managed by the IPv8 installer.' "$PANEL_ENV" || fail "$PANEL_ENV is unrelated; it was not overwritten."
  [[ ! -e $PANEL_UNIT ]] || grep -Fxq '# Managed by the IPv8 installer.' "$PANEL_UNIT" || fail "$PANEL_UNIT is unrelated; it was not overwritten."
  download_agent
  request_sudo
  as_root test ! -L /etc/ipv8 || fail '/etc/ipv8 is a symlink; refusing to follow it.'
  if as_root test -e /etc/ipv8; then as_root test -d /etc/ipv8 || fail '/etc/ipv8 is not a directory.'; fi
  as_root test ! -L "$PANEL_ENV" || fail "$PANEL_ENV is a symlink; refusing to follow it."
  as_root test ! -L "$PANEL_UNIT" || fail "$PANEL_UNIT is a symlink; refusing to follow it."
  if as_root test -e "$PANEL_ENV"; then as_root grep -Fxq '# Managed by the IPv8 installer.' "$PANEL_ENV" || fail "$PANEL_ENV is unrelated; it was not overwritten."; fi
  if as_root test -e "$PANEL_UNIT"; then as_root grep -Fxq '# Managed by the IPv8 installer.' "$PANEL_UNIT" || fail "$PANEL_UNIT is unrelated; it was not overwritten."; fi
  valid_prefix "$INSTALL_PREFIX" || fail 'installation prefix is unsafe; refusing to write files.'
  if as_root test -e "$INSTALL_PREFIX"; then as_root test -d "$INSTALL_PREFIX" && as_root test ! -L "$INSTALL_PREFIX" || fail 'installation prefix is not a safe directory.'; fi
  if as_root test -e "$INSTALL_PREFIX/bin"; then as_root test -d "$INSTALL_PREFIX/bin" && as_root test ! -L "$INSTALL_PREFIX/bin" || fail 'installation binary directory is not safe.'; fi
  as_root test ! -L /var/lib/ipv8 || fail '/var/lib/ipv8 is a symlink; refusing to use it.'
  as_root test ! -L "$IPV8_DATA" || fail 'the configured panel data path is a symlink.'
  if as_root test -e "$IPV8_DATA"; then as_root test -f "$IPV8_DATA" && as_root test ! -L "$IPV8_DATA" || fail 'the configured panel data path is not a safe regular file.'; fi
  if as_root test -e "$BIN"; then
    as_root test -f "$BIN" && as_root test ! -L "$BIN" || fail "$BIN is not a regular file."
    is_ipv8_binary || fail "$BIN is not a recognized IPv8 binary."
    backup_file "$BIN"
  fi
  if as_root test -e "$PANEL_ENV"; then backup_file "$PANEL_ENV"; fi
  if as_root test -e "$PANEL_UNIT"; then backup_file "$PANEL_UNIT"; fi
  cat >"$TMP_DIR/panel.env" <<EOF
# Managed by the IPv8 installer.
IPV8_ADDR=$IPV8_ADDR
IPV8_DATA=$IPV8_DATA
IPV8_PUBLIC_URL=$IPV8_PUBLIC_URL
IPV8_API_URL=$IPV8_API_URL
IPV8_COOKIE_SECURE=$IPV8_COOKIE_SECURE
EOF
  cat >"$TMP_DIR/panel.service" <<EOF
# Managed by the IPv8 installer.
[Unit]
Description=IPv8 experimental web panel
After=network.target

[Service]
Type=simple
DynamicUser=yes
StateDirectory=ipv8
EnvironmentFile=$PANEL_ENV
ExecStart=$BIN serve
Restart=on-failure
RestartSec=5
NoNewPrivileges=true
ProtectSystem=strict
ProtectHome=true
PrivateTmp=true
PrivateDevices=true
ProtectKernelTunables=true
ProtectKernelModules=true
ProtectControlGroups=true
CapabilityBoundingSet=
RestrictAddressFamilies=AF_UNIX AF_INET AF_INET6
LockPersonality=true

[Install]
WantedBy=multi-user.target
EOF
  SYSTEM_CHANGES=1
  as_root install -d -m 0755 "$INSTALL_PREFIX/bin" /etc/ipv8 /etc/systemd/system /var/lib/ipv8
  install -m 0755 "$AGENT" "$TMP_DIR/ipv8.new"
  as_root install -m 0755 "$TMP_DIR/ipv8.new" "$BIN"
  as_root install -m 0600 "$TMP_DIR/panel.env" "$PANEL_ENV"
  as_root install -m 0644 "$TMP_DIR/panel.service" "$PANEL_UNIT"
  as_root systemctl daemon-reload
  if ! as_root systemctl enable --now ipv8-panel.service; then
    printf 'Panel service did not start. Inspect: sudo journalctl -u ipv8-panel -n 50 --no-pager\n' >"$UI"
    fail 'panel service setup failed; config, database, and backup files were preserved.'
  fi
  local panel_state
  panel_state=$(as_root systemctl is-active ipv8-panel.service 2>/dev/null || printf inactive)
  if [[ $panel_state == active ]] && curl --proto '=http,https' --proto-redir '=https' --fail --silent --max-time 5 "http://$IPV8_ADDR/healthz" >"$TMP_DIR/health.json" 2>/dev/null; then
    printf 'Panel service: %s\nLocal health endpoint: HTTP 200 (%s)\n' "$panel_state" "$(cat "$TMP_DIR/health.json")" >"$UI"
  else
    printf 'Panel service: %s\nLocal health endpoint: unavailable. Inspect: sudo journalctl -u ipv8-panel -n 50 --no-pager\n' "$panel_state" >"$UI"
  fi
  printf 'Panel installation step completed. The database remains at %s; see README.md for first-admin setup.\n' "$IPV8_DATA" >"$UI"
}

show_status() {
  local panel='Not installed' agent='Not installed' health='Unavailable' version='Not installed' address
  if [[ -x $BIN ]]; then version=$($BIN version 2>/dev/null || printf Unknown); fi
  if [[ -f $PANEL_ENV && ! -e $PANEL_UNIT ]]; then panel='Unknown (configuration exists; no managed service)'; fi
  if [[ -e $PANEL_UNIT ]] && ! has_panel; then panel='Unknown (service unit is not managed by IPv8)'; fi
  if [[ -f $CONFIG && ! -e $UNIT ]]; then agent='Unknown (configuration exists; no managed service)'; fi
  if [[ -e $UNIT ]] && ! has_agent; then agent='Unknown (service unit is not managed by IPv8)'; fi
  if (( TERMUX )) && [[ -f $CONFIG ]]; then agent='Unknown (foreground process status is not tracked)'; fi
  if has_panel; then
    if command -v systemctl >/dev/null 2>&1 && [[ -d /run/systemd/system ]]; then panel=$(systemctl is-active ipv8-panel.service 2>/dev/null || printf inactive); else panel='Unknown (systemd unavailable)'; fi
    address=$(env_value "$PANEL_ENV" IPV8_ADDR 2>/dev/null || printf '127.0.0.1:8080')
    if ! valid_panel_addr "$address"; then health='Unknown (configured listen address is invalid; request skipped)'
    elif command -v curl >/dev/null 2>&1 && curl --proto '=http,https' --proto-redir '=https' --fail --silent --max-time 4 "http://$address/healthz" >/dev/null 2>&1; then health='HTTP 200 verified'
    else health='Unavailable (no local HTTP 200)'; fi
  fi
  if has_agent; then if command -v systemctl >/dev/null 2>&1 && [[ -d /run/systemd/system ]]; then agent=$(systemctl is-active ipv8-agent.service 2>/dev/null || printf inactive); else agent='Unknown (systemd unavailable)'; fi; fi
  printf '\nVerified host status\n  Panel service: %s\n  Local /healthz: %s\n  Agent service: %s\n  Installed binary: %s\n  Agent config: %s (%s)\n  Panel config: %s (%s)\n  Authenticated heartbeat: view in the panel; the installer has no admin session.\n' "$panel" "$health" "$agent" "$version" "$CONFIG" "$(test -f "$CONFIG" && printf exists || printf missing)" "$PANEL_ENV" "$(test -f "$PANEL_ENV" && printf exists || printf missing)" >"$UI"
  printf 'RTT, bandwidth, location, traffic history, and IPv8 packet routing are not implemented.\n' >"$UI"
}

uninstall_agent() {
  require_systemd || return 0
  if ! has_agent; then printf 'No IPv8-managed agent service was found.\n' >"$UI"; return 0; fi
  printf '\nAgent service: ipv8-agent.service\nBinary: %s\nUnit: %s\nCredential: %s (preserved by default)\nRevoke the enrollment in My IPv8 before removal where possible.\n' "$BIN" "$UNIT" "$CONFIG" >"$UI"
  ask 'Remove the agent service and binary? [y/N] '
  case "$REPLY" in y|Y|yes|YES) ;; *) printf 'Cancelled; nothing changed.\n' >"$UI"; return 0 ;; esac
  local delete_config=0
  ask 'Also delete the local enrollment credential? [y/N] '
  case "$REPLY" in y|Y|yes|YES) delete_config=1 ;; esac
  request_sudo
  as_root systemctl disable --now ipv8-agent.service
  as_root rm -f -- "$UNIT"
  if (( delete_config )) && as_root test -f "$CONFIG" && as_root test ! -L "$CONFIG"; then as_root rm -f -- "$CONFIG"; fi
  as_root systemctl daemon-reload
  remove_unused_binary
  as_root rmdir /etc/ipv8 2>/dev/null || :
  printf 'Agent service removed; the enrollment config was %s.\n' "$([[ $delete_config == 1 ]] && printf 'deleted by explicit request' || printf 'preserved')" >"$UI"
}

uninstall_panel() {
  require_systemd || return 0
  if ! has_panel; then printf 'No IPv8-managed panel service was found.\n' >"$UI"; return 0; fi
  local data delete_config=0 delete_data=0
  data=$(env_value "$PANEL_ENV" IPV8_DATA 2>/dev/null || printf '%s' "$PANEL_DATA")
  printf '\nPanel service: ipv8-panel.service\nBinary: %s\nUnit: %s\nConfig: %s (preserved by default)\nDatabase: %s (preserved by default)\n' "$BIN" "$PANEL_UNIT" "$PANEL_ENV" "$data" >"$UI"
  ask 'Remove the panel service and unit? [y/N] '
  case "$REPLY" in y|Y|yes|YES) ;; *) printf 'Cancelled; nothing changed.\n' >"$UI"; return 0 ;; esac
  ask 'Also delete panel configuration? [y/N] '; case "$REPLY" in y|Y|yes|YES) delete_config=1 ;; esac
  if valid_data_path "$data"; then ask 'Also permanently delete the database file? Type DELETE to confirm: '; [[ $REPLY == DELETE ]] && delete_data=1; fi
  request_sudo
  as_root systemctl disable --now ipv8-panel.service
  as_root rm -f -- "$PANEL_UNIT"
  if (( delete_config )) && as_root test -f "$PANEL_ENV" && as_root test ! -L "$PANEL_ENV"; then as_root rm -f -- "$PANEL_ENV"; fi
  if (( delete_data )) && as_root test -f "$data" && as_root test ! -L "$data"; then as_root rm -f -- "$data"; fi
  as_root systemctl daemon-reload
  remove_unused_binary
  as_root rmdir /etc/ipv8 2>/dev/null || :
  printf 'Panel service removed. Database/config preservation was selected unless explicitly confirmed above.\n' >"$UI"
}

update_flow() {
  require_systemd || return 0
  if ! has_panel && ! has_agent; then printf 'No installer-managed IPv8 service was found to update.\n' >"$UI"; return 0; fi
  is_ipv8_binary || { printf 'The installed binary is missing or not verifiably an IPv8 executable; refusing an in-place update.\n' >"$UI"; return 0; }
  printf '\nThe panel and agent share one executable at %s. Configurations and database are not changed. A binary backup is made before replacement.\n' "$BIN" >"$UI"
  [[ ! -x $BIN ]] || printf 'Current version: %s\n' "$($BIN version 2>/dev/null || printf Unknown)" >"$UI"
  local release_meta release_tag
  release_meta=$(curl --proto '=https' --tlsv1.2 --connect-timeout 5 --max-time 10 --fail --silent --show-error "$REPO/releases/latest" 2>/dev/null || :)
  release_tag=$(printf '%s\n' "$release_meta" | sed -n 's/^[[:space:]]*"tag_name":[[:space:]]*"\([^"]*\)".*/\1/p' | sed -n '1p')
  if [[ $release_tag =~ ^v?[A-Za-z0-9._-]{1,64}$ ]]; then
    printf 'Latest published release: %s\nChanges: %s/releases/tag/%s\n' "$release_tag" "$REPO" "$release_tag" >"$UI"
  else
    printf 'Published release: Unavailable (GitHub reports none or lookup failed); after confirmation the installer may offer a Go source build.\n' >"$UI"
  fi
  ask 'Check for a verified release or offered Go source build, then update the shared binary? [y/N] '
  case "$REPLY" in y|Y|yes|YES) ;; *) printf 'Update cancelled; no download or changes made.\n' >"$UI"; return 0 ;; esac
  local was_panel=0 was_agent=0
  if has_panel && systemctl is-active --quiet ipv8-panel.service; then was_panel=1; fi
  if has_agent && systemctl is-active --quiet ipv8-agent.service; then was_agent=1; fi
  check_prerequisites || fail 'update prerequisites failed.'
  valid_prefix "$INSTALL_PREFIX" || fail 'installation prefix is unsafe; refusing to update files.'
  download_agent
  request_sudo
  if as_root test -e "$BIN"; then as_root test -f "$BIN" && as_root test ! -L "$BIN" || fail "$BIN is not a regular file."; backup_file "$BIN"; fi
  install -m 0755 "$AGENT" "$TMP_DIR/ipv8.new"
  as_root install -m 0755 "$TMP_DIR/ipv8.new" "$BIN"
  if ! "$BIN" version >/dev/null 2>&1; then
    as_root install -m 0755 "$BACKUP_PATH" "$BIN"
    printf 'The new binary failed its version check. The previous binary was restored.\n' >"$UI"
    return 0
  fi
  if (( was_panel )) && ! as_root systemctl restart ipv8-panel.service; then
    as_root install -m 0755 "$BACKUP_PATH" "$BIN"
    as_root systemctl restart ipv8-panel.service || :
    (( ! was_agent )) || as_root systemctl restart ipv8-agent.service || :
    printf 'Panel restart failed; the previous binary was restored. Inspect ipv8-panel logs.\n' >"$UI"
    return 0
  fi
  if (( was_agent )) && ! as_root systemctl restart ipv8-agent.service; then
    as_root install -m 0755 "$BACKUP_PATH" "$BIN"
    (( ! was_panel )) || as_root systemctl restart ipv8-panel.service || :
    as_root systemctl restart ipv8-agent.service || :
    printf 'Agent restart failed; the previous binary was restored. Inspect ipv8-agent logs.\n' >"$UI"
    return 0
  fi
  if (( was_panel )); then
    local health_addr
    health_addr=$(env_value "$PANEL_ENV" IPV8_ADDR 2>/dev/null || printf '127.0.0.1:8080')
    if ! valid_panel_addr "$health_addr" || ! curl --proto '=http,https' --proto-redir '=https' --fail --silent --max-time 5 "http://$health_addr/healthz" >/dev/null 2>&1; then
      as_root install -m 0755 "$BACKUP_PATH" "$BIN"
      as_root systemctl restart ipv8-panel.service || :
      (( ! was_agent )) || as_root systemctl restart ipv8-agent.service || :
      printf 'Post-update panel health failed; previous binary restored. Inspect ipv8-panel logs.\n' >"$UI"
      return 0
    fi
  fi
  printf 'Updated from %s\nInstalled version: %s\nRollback backup: %s\n' "$RELEASE_KIND" "$($BIN version 2>/dev/null)" "$BACKUP_PATH" >"$UI"
}

show_logs() {
  require_systemd || { printf 'Termux agent logs are in the foreground terminal; systemd journal is unavailable.\n' >"$UI"; return 0; }
  local units=()
  has_panel && units+=(-u ipv8-panel.service)
  has_agent && units+=(-u ipv8-agent.service)
  if ((${#units[@]} == 0)); then printf 'No IPv8-managed systemd services were found. Read README.md troubleshooting for setup checks.\n' >"$UI"; return 0; fi
  journalctl "${units[@]}" -n 50 --no-pager >"$UI" 2>&1 || printf 'Use sudo journalctl -u ipv8-panel -u ipv8-agent -n 50 --no-pager\n' >"$UI"
}

run_menu() {
  valid_prefix "$INSTALL_PREFIX" || fail 'IPV8_INSTALL_PREFIX is not a safe absolute path.'
  while :; do
    menu_banner
    printf '[1] Install IPv8 Panel\n[2] Install IPv8 Agent\n[3] Configure IPv8\n[4] Uninstall IPv8 Agent\n[5] Uninstall IPv8 Panel\n[6] View Status & Health\n[7] View Network Information\n[8] DNS Integration Guide\n[9] Check System Requirements\n[10] Update IPv8\n[11] View Logs & Troubleshooting\n[12] Exit\n\n' >"$UI"
    ask 'Choose an option [1-12]: '
    case "$REPLY" in
      1) install_panel_flow; pause_menu ;;
      2) install_flow; pause_menu ;;
      3) configure_panel_menu; pause_menu ;;
      4) uninstall_agent; pause_menu ;;
      5) uninstall_panel; pause_menu ;;
      6) show_status; pause_menu ;;
      7) show_information; pause_menu ;;
      8) show_dns_guide; pause_menu ;;
      9) if check_prerequisites; then :; else printf 'One or more requirements are missing. No changes were made.\n' >"$UI"; fi; pause_menu ;;
      10) update_flow; pause_menu ;;
      11) show_logs; pause_menu ;;
      12) printf 'Goodbye. The menu did not make system changes unless an installation/configuration action was confirmed.\n' >"$UI"; return 0 ;;
      *) printf 'Invalid selection. Enter a number from 1 to 12.\n' >"$UI" ;;
    esac
  done
}

if (( DRY_RUN )); then
  valid_name "$NAME" || fail 'node name must contain 1-60 printable characters.'
  valid_prefix "$INSTALL_PREFIX" || fail 'prefix must be an absolute safe path.'
  [[ -z $SERVER ]] || valid_server "$SERVER" || fail 'server must be a valid HTTPS origin or localhost prototype.'
  check_prerequisites || fail 'prerequisite checks failed.'
  verify_install_targets
  check_disk_space 20480
  printf 'Dry run passed for %s/%s. No downloads, token use, package installation, or files/services changed.\n' "$GOOS" "$ARCH"
  exit 0
fi

if (( MENU || ARG_COUNT == 0 )); then run_menu; else
  [[ $UI == /dev/tty ]] || fail 'an interactive terminal is required; no software was downloaded.'
  install_flow
fi
