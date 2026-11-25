#!/bin/sh
# Tries the dashboard's install command on a throwaway Linux container, so you do not need a spare server.
#   sh deploy/docker/try/try-install.sh '<the command copied from Hosts > Add a server>' [--ai]
# The container has a stand-in for systemd that starts the agents the way systemd would. Ctrl-C stops and removes it.
set -eu
export MSYS_NO_PATHCONV=1 MSYS2_ARG_CONV_EXCL='*'
cd "$(dirname "$0")/.."
cmd=${1:?paste the install command from the Hosts page, in quotes}
shift
# inside a container "localhost" is the container itself, and there is no sudo
cmd=$(printf '%s' "$cmd" | sed 's#https://localhost:8080#https://host.docker.internal:8080#g; s#| sudo sh#| sh#')
# a self-signed server needs its certificate handed to the agents too
cmd="$cmd --cacert /server.crt"
crt=$(pwd -W 2>/dev/null || pwd)/certs/server.crt

if tty -s; then T=-it; else T=-i; fi
docker run --rm $T --name aero-try-install -e AERO_TRY_NOWAIT -e CURL_CA_BUNDLE=/server.crt -e AERO_AI_PROVIDER -e AERO_AI_MODEL -e AERO_AI_KEY \
	-v "$crt:/server.crt:ro" debian:bookworm-slim sh -c '
set -e
apt-get update -qq >/dev/null && apt-get install -y -qq curl openssl ca-certificates >/dev/null 2>&1
mkdir -p /run/systemd/system
# stand-in for systemd: starts a service the way its unit would, from the unit and env file the installer wrote
cat >/usr/bin/systemctl <<'"'"'SC'"'"'
#!/bin/sh
[ "$1" = enable ] || exit 0
svc=$3; set -a; . /etc/${svc%%.*}/env 2>/dev/null || . /etc/aero-agent/env; set +a
case $svc in
aero-agent) AERO_GATEWAY=$AERO_GATEWAY; nohup /usr/local/bin/aero-agent "$AERO_GATEWAY" 5 >/var/log/$svc.log 2>&1 & ;;
aero-remediation) STATE_DIRECTORY=/var/lib/aero-remediation; mkdir -p $STATE_DIRECTORY; nohup /usr/local/bin/aero-remediation >/var/log/$svc.log 2>&1 & ;;
esac
SC
chmod +x /usr/bin/systemctl
'"$cmd"'
sleep 5
for f in /var/log/aero-*.log; do [ -f "$f" ] && { echo "--- $f"; tail -4 "$f"; }; done
echo; echo "The agents are running inside this container. Watch your dashboard. Press Ctrl-C to stop and remove it."
[ -n "$AERO_TRY_NOWAIT" ] || sleep 100000
'
