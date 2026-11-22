#!/bin/sh
# Makes a self-signed certificate for the names or IPs you pass, in ./certs.
#   ./gen-certs.sh localhost 203.0.113.7 aero.example.com
# Agents then trust it with AERO_CA_CERT=certs/server.crt. For production, put a real certificate
# at certs/server.crt and certs/server.key instead (for example from Let's Encrypt).
set -eu
# Git Bash on Windows would otherwise rewrite the /CN=... argument as a file path
export MSYS_NO_PATHCONV=1 MSYS2_ARG_CONV_EXCL='*'
[ "$#" -ge 1 ] || { echo "usage: $0 <hostname-or-ip>..." >&2; exit 2; }
cd "$(dirname "$0")"
mkdir -p certs

san=""
for name in "$@"; do
  case "$name" in
    *[!0-9.]*) san="$san,DNS:$name" ;;  # anything with a letter is a DNS name
    *) san="$san,IP:$name" ;;
  esac
done
san="${san#,}"

umask 077
if ! err=$(openssl req -x509 -newkey rsa:3072 -sha256 -nodes -days 825 \
  -keyout certs/server.key -out certs/server.crt \
  -subj "/CN=$1" -addext "subjectAltName=$san" -addext "basicConstraints=critical,CA:FALSE" 2>&1); then
  rm -f certs/server.key certs/server.crt
  echo "openssl failed:" >&2
  echo "$err" >&2
  exit 1
fi
chmod 644 certs/server.crt
echo "wrote certs/server.crt and certs/server.key for: $san"
