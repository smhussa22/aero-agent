# Installing the agents on a server

Two small programs run on each server you want to watch. Both connect **out** to your Aero stack, so the
server needs no inbound port.

| Program | Does | Needs |
|---|---|---|
| `aero-agent` (C++) | Reads CPU, memory, disk, network and process numbers and streams them to your stack | nothing special: no privileges |
| `aero-remediation` (Go) | Investigates alerts with **your** AI key and runs approved fixes | optional; sudo rules for the fixes you allow |

Start with the first. The second is optional and only makes sense once you have a dashboard.

You need, from your stack (see `deploy/README.md`): its address, the certificate (`server.crt`, public), and a token
for this server from `hostctl add <name>`. Use one token per server; the token also fixes which host name the
server may report as.

Releases (pushed tags `v*`) publish static `aero-agent` and `aero-remediation` binaries for amd64 and arm64, with a signed `SHA256SUMS`. Below is the build-from-source route, tested on Ubuntu 24.04 on x86-64 only.
Other distributions and ARM (including a Raspberry Pi) should work but have not been tried.

## Quick install (one line)

On the dashboard, open **Hosts → Add a server**, choose **Make an install command** (tick **Also install the AI agent**
if you want it) and copy the command. It is single-use and expires in 15 minutes. Run it as root on the server:

```sh
curl -fsSL https://your-aero/install.sh | sudo sh -s -- --server https://your-aero --code <code> [--name web-1] [--ai]
```

The binaries come from your own server (`/releases/`, built with `deploy/docker/try/build-release.sh`) or, if it hosts
none, from the project's GitHub releases. Nothing is installed unless the checksum list carries the release signature
and every file matches it. The server needs `GATEWAY_PUBLIC_ADDR` set. With `--ai` the installer asks for your AI
provider and key on the server (or reads `AERO_AI_PROVIDER`, `AERO_AI_MODEL` and `AERO_AI_KEY`). The key never leaves
that machine, and the agent starts in observe-only mode. To try this on one machine, see `deploy/docker/try/README.md`.

## 1. Monitoring agent

```sh
sudo apt-get install -y g++-14 cmake pkg-config libgrpc++-dev libprotobuf-dev protobuf-compiler protobuf-compiler-grpc
git clone https://github.com/smhussa22/aero-agent.git && cd aero-agent
cmake -S agent -B build -DCMAKE_CXX_COMPILER=g++-14 -DCMAKE_BUILD_TYPE=Release
cmake --build build -j"$(nproc)"
ctest --test-dir build                                  # optional: runs the parser tests
sudo install -m 755 build/aero-agent /usr/local/bin/
```

Configure and run it as a locked-down service:

```sh
sudo install -d -m 755 /etc/aero-agent
sudo install -m 644 server.crt /etc/aero-agent/server.crt
sudo sh -c 'umask 077; cat > /etc/aero-agent/env' <<'EOF'
AERO_GATEWAY=your-host:50051
AERO_TOKEN=paste-the-token-here
AERO_CA_CERT=/etc/aero-agent/server.crt
EOF
sudo install -m 644 agent/deploy/aero-agent.service /etc/systemd/system/
sudo systemctl enable --now aero-agent
journalctl -u aero-agent -f
```

If your stack has a real certificate (for example Let's Encrypt), leave `AERO_CA_CERT` out. The agent uses TLS
unless you set `AERO_INSECURE=1`, which is for local development only and sends the token in cleartext.

Within a minute the host appears on the dashboard's Hosts page.

## 2. Remediation agent (optional)

It needs Go and the generated protobuf code:

```sh
sudo apt-get install -y protobuf-compiler libprotobuf-dev
go install google.golang.org/protobuf/cmd/protoc-gen-go@latest
go install google.golang.org/grpc/cmd/protoc-gen-go-grpc@latest
make -C backend proto
cd remediation-agent && go build -o aero-remediation ./cmd/aero-remediation
sudo install -m 755 aero-remediation /usr/local/bin/
```

Create its account, decide what it may do, and configure it:

```sh
sudo useradd --system --no-create-home --shell /usr/sbin/nologin aero-remediation
sudo install -d -o aero-remediation -m 700 /var/lib/aero-remediation
sudo install -d -m 755 /etc/aero-remediation
sudo sh -c 'umask 077; cat > /etc/aero-remediation/env' <<'EOF'
AERO_ORCH_ADDR=your-host:50052
AERO_TOKEN=paste-the-same-token-here
AERO_CA_CERT=/etc/aero-agent/server.crt
AERO_HOST_ID=web-1
AERO_MAX_MODE=observe
AERO_ALLOWED_SERVICES=nginx
AERO_SUDO=1
AERO_AI_PROVIDER=anthropic
AERO_AI_MODEL=<a model your key can use>
AERO_AI_KEY=paste-your-provider-key-here
EOF
sudo install -m 644 remediation-agent/deploy/aero-remediation.service /etc/systemd/system/
sudo systemctl enable --now aero-remediation
```

The settings that matter most:

- `AERO_MAX_MODE` is a **ceiling set on this machine**. It defaults to `observe`: the agent investigates and
  explains but changes nothing. The dashboard can lower the mode for this host, never raise it above the
  ceiling, so a compromised dashboard cannot grant powers you did not give. Move to `recommend` (every fix
  needs your approval) or `autonomous` (safe fixes run alone, dangerous ones still ask) deliberately.
- `AERO_ALLOWED_SERVICES` lists the only services `restart_service` may touch. Empty means none.
- `AERO_SUDO=1` runs fixes through `sudo -n`. Install `remediation-agent/deploy/aero-remediation.sudoers` to
  `/etc/sudoers.d/aero-remediation` (check it first with `visudo -cf`), and keep its service list identical
  to `AERO_ALLOWED_SERVICES`. Read the comment about `kill`: sudoers cannot limit which process id is signalled.
- `AERO_AI_*` is your own provider and key. Aero's servers never see it. The key sits in the root-only
  `/etc/aero-remediation/env`; keep that file at mode 600 and out of backups you share.
- `AERO_DRY_RUN=1` makes it log what it would do and run nothing: a good first step before `recommend`.

Every decision is written to `/var/lib/aero-remediation/audit.log`, a hash-chained file that shows if lines are
edited or removed: `audit-verify /var/lib/aero-remediation/audit.log`.

## Rotating or revoking a server

`hostctl add <name>` on an existing name issues a new token and the old one stops working for new connections.
**An already-open investigations connection keeps running until it reconnects**, so restart the server's
`aero-remediation` (or the orchestrator) to cut it off immediately.
