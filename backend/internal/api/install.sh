#!/bin/sh
# Installs the Aero monitoring agent on this Linux server and connects it to your account.
#   curl -fsSL <server>/install.sh | sudo sh -s -- --server <server> --code <code> [--name <name>] [--ai] [--cacert <file>]
# The code is single-use and expires in minutes; the server trades it for this machine's own token.
# --ai also installs the AI remediation agent (observe-only: it investigates and explains, it changes nothing). It asks
# for your AI provider and key, or reads AERO_AI_PROVIDER, AERO_AI_MODEL and AERO_AI_KEY. The key stays on this machine.
set -eu

server= code= name=$(hostname) cacert= ai=0
while [ $# -gt 0 ]; do
	case $1 in
	--ai) ai=1; shift; continue ;;
	--server) server=${2:?} ;;
	--code) code=${2:?} ;;
	--name) name=${2:?} ;;
	--cacert) cacert=${2:?} ;;
	*) echo "unknown option: $1" >&2; exit 2 ;;
	esac
	shift 2
done
[ -n "$server" ] && [ -n "$code" ] || { echo "usage: install.sh --server <url> --code <code> [--name <name>] [--ai] [--cacert <file>]" >&2; exit 2; }
server=${server%/}
[ "$(id -u)" = 0 ] || { echo "run as root (use sudo)" >&2; exit 1; }
[ "$(uname -s)" = Linux ] || { echo "Linux only" >&2; exit 1; }
[ -d /run/systemd/system ] || { echo "systemd is required (this machine is not running it)" >&2; exit 1; }
for c in curl openssl sha256sum; do command -v "$c" >/dev/null || { echo "$c is required" >&2; exit 1; }; done

case $(uname -m) in
x86_64 | amd64) arch=amd64 ;;
aarch64 | arm64) arch=arm64 ;;
*) echo "unsupported CPU: $(uname -m)" >&2; exit 1 ;;
esac

# curl that trusts the server's own certificate when one is given (a self-signed setup)
cu() { if [ -n "$cacert" ]; then curl -fsSL --cacert "$cacert" "$@"; else curl -fsSL "$@"; fi; }

tmp=$(mktemp -d)
trap 'rm -rf "$tmp"' EXIT

# binaries come from your own server when it hosts them, otherwise from the public releases
if [ -n "${AERO_RELEASE_BASE:-}" ]; then
	base=$AERO_RELEASE_BASE
elif cu -o /dev/null "$server/releases/SHA256SUMS" 2>/dev/null; then
	base=$server/releases
else
	base=https://github.com/smhussa22/aero-agent/releases/latest/download
fi

# nothing is installed unless the checksum list carries our release signature and every file matches it
cu "$base/SHA256SUMS" -o "$tmp/SHA256SUMS"
cu "$base/SHA256SUMS.sig" -o "$tmp/SHA256SUMS.sig"
cat >"$tmp/release.pub" <<'KEY'
-----BEGIN PUBLIC KEY-----
MCowBQYDK2VwAyEAEyPG6LtGQNgGJ9jd07YSfF2s3dZcom+9lX7bVVYFlzQ=
-----END PUBLIC KEY-----
KEY
openssl pkeyutl -verify -pubin -inkey "$tmp/release.pub" -rawin -in "$tmp/SHA256SUMS" -sigfile "$tmp/SHA256SUMS.sig" >/dev/null 2>&1 ||
	{ echo "release signature is invalid (or openssl is older than 3.0): refusing to install" >&2; exit 1; }

fetch() { # name of the release file, saved under $tmp
	cu "$base/$1-linux-$arch" -o "$tmp/$1"
	want=$(sed -n "s/^\([0-9a-f]\{64\}\) [ *]$1-linux-$arch\$/\1/p" "$tmp/SHA256SUMS")
	have=$(sha256sum "$tmp/$1" | cut -d' ' -f1)
	[ -n "$want" ] && [ "$want" = "$have" ] || { echo "checksum mismatch for $1: refusing to install" >&2; exit 1; }
}
fetch aero-agent
[ "$ai" = 1 ] && fetch aero-remediation

# collect the AI settings first, so a mistake costs nothing
if [ "$ai" = 1 ]; then
	ask() { # prompt, default -> reads from the terminal even though the script itself arrives on stdin
		if [ -r /dev/tty ]; then printf '%s%s: ' "$1" "${2:+ [$2]}" >/dev/tty; read -r v </dev/tty || v=; fi
		echo "${v:-$2}"
	}
	provider=${AERO_AI_PROVIDER:-$(ask "AI provider (anthropic or openai)" anthropic)}
	case $provider in anthropic) dm=claude-haiku-5-5 ;; *) dm= ;; esac
	model=${AERO_AI_MODEL:-$(ask "Model" "$dm")}
	key=${AERO_AI_KEY:-}
	if [ -z "$key" ] && [ -r /dev/tty ]; then
		printf 'API key (hidden): ' >/dev/tty; stty -echo </dev/tty 2>/dev/null || true
		read -r key </dev/tty || key=; stty echo </dev/tty 2>/dev/null || true; echo >/dev/tty
	fi
	[ -n "$provider" ] && [ -n "$model" ] && [ -n "$key" ] || { echo "the AI agent needs a provider, model and key (or set AERO_AI_PROVIDER, AERO_AI_MODEL, AERO_AI_KEY)" >&2; exit 1; }
fi

# trade the code for this server's token
reply=$(cu -X POST "$server/enroll" -H 'Content-Type: application/json' -d "{\"code\":\"$code\",\"name\":\"$name\"}") ||
	{ echo "enrollment failed: the code may be expired or already used. Make a new one on the Hosts page." >&2; exit 1; }
field() { printf '%s' "$reply" | sed -n "s/.*\"$1\":\"\([^\"]*\)\".*/\1/p"; }
token=$(field token) gateway=$(field gateway) orch=$(field orchestrator)
[ -n "$token" ] && [ -n "$gateway" ] || { echo "unexpected reply from the server" >&2; exit 1; }

# the root-only env files hold the token (and the key); the units load them, nothing sensitive sits in a unit
cacert_line() { [ -n "$cacert" ] && echo "AERO_CA_CERT=/etc/aero-agent/server.crt"; true; }
install -d -m 755 /etc/aero-agent
[ -n "$cacert" ] && install -m 644 "$cacert" /etc/aero-agent/server.crt
(umask 077; { echo "AERO_GATEWAY=$gateway"; echo "AERO_TOKEN=$token"; cacert_line; } >/etc/aero-agent/env)
install -m 755 "$tmp/aero-agent" /usr/local/bin/aero-agent

cat >/etc/systemd/system/aero-agent.service <<'UNIT'
[Unit]
Description=Aero Agent monitoring agent
After=network-online.target
Wants=network-online.target

[Service]
EnvironmentFile=/etc/aero-agent/env
ExecStart=/usr/local/bin/aero-agent ${AERO_GATEWAY} 5
Restart=always
RestartSec=5
DynamicUser=yes
NoNewPrivileges=true
CapabilityBoundingSet=
AmbientCapabilities=
ProtectSystem=strict
ProtectHome=true
PrivateTmp=true
PrivateDevices=true
ProtectKernelTunables=true
ProtectKernelModules=true
ProtectControlGroups=true
RestrictAddressFamilies=AF_INET AF_INET6
RestrictNamespaces=true
LockPersonality=true
MemoryDenyWriteExecute=true
SystemCallArchitectures=native
MemoryMax=128M

[Install]
WantedBy=multi-user.target
UNIT

if [ "$ai" = 1 ]; then
	id aero-remediation >/dev/null 2>&1 || useradd --system --no-create-home --shell /usr/sbin/nologin aero-remediation
	install -m 755 "$tmp/aero-remediation" /usr/local/bin/aero-remediation
	install -d -m 755 /etc/aero-remediation
	(
		umask 077
		{
			echo "AERO_ORCH_ADDR=$orch"
			echo "AERO_TOKEN=$token"
			echo "AERO_HOST_ID=$name"
			echo "AERO_MAX_MODE=observe"
			echo "AERO_AI_PROVIDER=$provider"
			echo "AERO_AI_MODEL=$model"
			echo "AERO_AI_KEY=$key"
			[ -n "$cacert" ] && echo "AERO_CA_CERT=/etc/aero-agent/server.crt"
			true
		} >/etc/aero-remediation/env
	)
	cat >/etc/systemd/system/aero-remediation.service <<'UNIT'
[Unit]
Description=Aero Agent remediation agent
After=network-online.target

[Service]
User=aero-remediation
Group=aero-remediation
EnvironmentFile=/etc/aero-remediation/env
ExecStart=/usr/local/bin/aero-remediation
Restart=on-failure
RestartSec=5
StateDirectory=aero-remediation
ProtectHome=true
PrivateTmp=true
ProtectKernelModules=true
ProtectKernelTunables=true
ProtectControlGroups=true
RestrictAddressFamilies=AF_INET AF_INET6 AF_UNIX
LockPersonality=true
MemoryMax=256M

[Install]
WantedBy=multi-user.target
UNIT
fi

systemctl daemon-reload
systemctl enable --now aero-agent
[ "$ai" = 1 ] && systemctl enable --now aero-remediation
echo "done: '$name' will appear on your dashboard within a minute."
[ "$ai" = 1 ] && echo "The AI agent runs in observe mode: it investigates and explains alerts but changes nothing. Raise it on the Hosts page."
echo "Logs: journalctl -u aero-agent -f"
exit 0
