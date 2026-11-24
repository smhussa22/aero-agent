#!/bin/sh
# Builds the agent binaries your own server hands out at /releases/, with a signed checksum list.
#   AERO_RELEASE_KEY=path/to/private-key.pem sh deploy/docker/try/build-release.sh
# Needs Docker. The signing key's public half is embedded in backend/internal/api/install.sh.
set -eu
export MSYS_NO_PATHCONV=1 MSYS2_ARG_CONV_EXCL='*'
cd "$(dirname "$0")/../../.."
src=$(pwd -W 2>/dev/null || pwd)
out=deploy/docker/releases
key=${AERO_RELEASE_KEY:?set AERO_RELEASE_KEY to the signing private key (PEM)}
key=$(cygpath -w "$key" 2>/dev/null || echo "$key") # a native Windows openssl needs a Windows path
case $(docker info --format '{{.Architecture}}') in x86_64) arch=amd64 ;; aarch64 | arm64) arch=arm64 ;; *) echo "unsupported Docker host CPU" >&2; exit 1 ;; esac

mkdir -p "$out" "$out/.tmp"
echo "building the static monitoring agent (the first build compiles gRPC and takes a while)..."
docker build -f deploy/docker/Dockerfile.agent -o "$out/.tmp" .
mv "$out/.tmp/aero-agent" "$out/aero-agent-linux-$arch"
rmdir "$out/.tmp"

echo "building the remediation agent..."
docker run --rm -v "$src:/src" -w /src/remediation-agent -e CGO_ENABLED=0 -e GOOS=linux -e "GOARCH=$arch" golang:1.26 \
	go build -trimpath -ldflags "-s -w" -o "/src/$out/aero-remediation-linux-$arch" ./cmd/aero-remediation

cd "$out"
sha256sum aero-*-linux-* | sed 's/ \*/  /' >SHA256SUMS
openssl pkeyutl -sign -inkey "$key" -rawin -in SHA256SUMS -out SHA256SUMS.sig
ls -la
