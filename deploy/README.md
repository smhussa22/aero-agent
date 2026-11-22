# Deploying Aero Agent

Two ways to run the platform. Both end up as the same Docker Compose stack.

| | Where | When |
|---|---|---|
| **Docker Compose** (`docker/`) | any Linux box with Docker | homelab, a VPS you already have |
| **Terraform on AWS** (`terraform/`) | one EC2 instance running that same stack | you want a server provisioned for you |

The monitoring agent (`agent/`) and the remediation agent (`remediation-agent/`) run **on the servers you
monitor**, not in this stack. They connect out to it.

## Docker Compose

```sh
cd deploy/docker
./gen-certs.sh localhost 203.0.113.7 aero.example.com   # names and IPs agents and browsers will use
cp .env.example .env                                    # then set POSTGRES_PASSWORD
docker compose up -d --build
```

Create the first admin and register a server (the password is read from stdin, so it never lands in shell history):

```sh
echo 'a long passphrase' | docker compose run --rm -T tools userctl create you@example.com admin
docker compose run --rm -T tools hostctl add web-1     # prints that server's token once
```

Open `https://<your-host>:8080`. On each monitored server:

```sh
AERO_TOKEN=<token> AERO_CA_CERT=server.crt aero-agent <your-host>:50051            # metrics
AERO_TOKEN=<token> AERO_CA_CERT=server.crt AERO_ORCH_ADDR=<your-host>:50052 \
  AERO_AI_PROVIDER=anthropic AERO_AI_MODEL=... AERO_AI_KEY=... aero-remediation    # AI investigations
```

`server.crt` is `deploy/docker/certs/server.crt`, copied to the server. For production, replace
`certs/server.crt` and `certs/server.key` with a real certificate (for example Let's Encrypt) and drop `AERO_CA_CERT`.

What is exposed: the web app and API on 8080, and the agent ports 50051 (metrics) and 50052 (investigations).
The database and Kafka are on the private compose network only. Every service runs as an unprivileged user with
a read-only filesystem and no extra capabilities.

**Known limit:** traffic between containers on the private network (to Postgres and Kafka) is not encrypted.
It never leaves the host, but add TLS if you split the stack across machines.

Back up the `pgdata` volume. Upgrade with `git pull && docker compose up -d --build`; migrations run first.

## Terraform on AWS

See `terraform/README.md`.
