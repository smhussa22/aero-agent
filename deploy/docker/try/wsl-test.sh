#!/bin/sh
# Sets up the remediation test on this machine: a CPU-burning service the AI agent may restart, and recommend mode.
# Run as root:  sudo sh deploy/docker/try/wsl-test.sh
set -eu
[ "$(id -u)" = 0 ] || { echo "run with sudo" >&2; exit 1; }
env=/etc/aero-remediation/env
[ -f "$env" ] || { echo "$env missing: install with --ai first" >&2; exit 1; }
command -v stress-ng >/dev/null || { apt-get update -qq && apt-get install -y -qq stress-ng; }

# a service to burn CPU, started fresh
systemctl stop burn 2>/dev/null || true
systemctl reset-failed burn 2>/dev/null || true
systemd-run --unit=burn stress-ng --cpu 2 --timeout 0

# set each key once (drop any old line first)
setkey() { sed -i "/^$1=/d" "$env"; echo "$1=$2" >>"$env"; }
setkey AERO_MAX_MODE recommend
setkey AERO_ALLOWED_SERVICES burn
setkey AERO_SUDO 1

# let the agent restart only the burn service
tmp=$(mktemp)
echo 'aero-remediation ALL=(root) NOPASSWD: /usr/bin/systemctl restart -- burn' >"$tmp"
visudo -cf "$tmp"
install -m 440 "$tmp" /etc/sudoers.d/aero-remediation
rm -f "$tmp"

systemctl restart aero-remediation
sleep 4
systemctl is-active burn aero-agent aero-remediation
journalctl -u aero-remediation -n 15 --no-pager
