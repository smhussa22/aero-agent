# Trying Aero Agent on one machine

Needs Docker. Everything here is for local trial only (throwaway secrets, a self-signed certificate).

```sh
cd deploy/docker
sh gen-certs.sh localhost host.docker.internal
cp try/env.try .env                  # then set SUPABASE_URL and SUPABASE_ANON_KEY (see .env.example)
AERO_RELEASE_KEY=<signing key pem> sh try/build-release.sh   # builds the agent binaries your server hands out
docker compose -f docker-compose.yml -f docker-compose.try.yml up -d --build
```

Open https://localhost:8080 (accept the certificate warning) and create an account.

**Connect a server.** On the Hosts page choose "Make an install command", optionally tick "Also install the AI agent",
and copy the command. To try it without a spare server, run it in a throwaway Linux container:

```sh
sh try/try-install.sh '<the command you copied>'          # add AERO_AI_PROVIDER, AERO_AI_MODEL, AERO_AI_KEY to the
                                                          # environment first if you ticked the AI agent
```

The container stays up so you can watch the server appear on the dashboard. Ctrl-C removes it.
