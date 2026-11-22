# Threat model

What Aero Agent protects, from whom, how, and what it does **not** protect against. Written against the
code as it stands; where something is a gap it says so. If you find one that is not listed, see `SECURITY.md`.

## What an attacker would want

| Asset | Why it matters |
|---|---|
| **Ability to run commands on monitored servers** | The worst outcome. The product exists to change servers. |
| **Your AI provider key** | Spend on your account; reading what you send the model. |
| **Host tokens** | Report fake metrics as a server, or open an investigations connection as it. |
| **Dashboard sessions and passwords** | Approve fixes, edit rules, read server data. |
| **Integrity of the audit trail** | To hide what happened. |
| **Server and metric data** | Reconnaissance: what runs where, and how loaded it is. |

## Who and where (trust boundaries)

```
 browser ──https──▶ API ────────┐
                                ├──▶ Postgres / TimescaleDB, Kafka   (private network, one host)
 monitoring agent ──tls+token──▶ ingest ─▶ Kafka ─▶ writer, alert engine
 remediation agent ──tls+token─▶ orchestrator ─▶ Postgres
 remediation agent ──https────▶ your AI provider (with your key)
 browser ──https───────────────▶ your AI provider (chat, with your key)
```

Treated as **untrusted**: everything on a monitored server (process names, log lines, file contents),
everything an AI model returns, anything a browser sends, anything in an imported layout.
Treated as **trusted**: the machine running the stack, and the root user on a monitored server.
A compromise of either is out of scope (see "Not defended").

## Threats and what answers them

### An AI model, or text it reads, makes the agent do something harmful
- The AI never gets a shell. It names an action from a fixed allowlist (restart a service, signal a process,
  trim the journal) and fills parameters. Parameters are pattern- and range-checked; shell metacharacters,
  unknown actions and unknown fields are rejected, and the executor builds an argument list rather than a shell string.
- Everything gathered from the server is fenced as untrusted data in the prompt, with the fence markers and control
  characters stripped, secrets redacted, and sizes capped. A reply is parsed strictly; at most 5 proposals survive.
- Whether something is "dangerous" comes from the catalog, never from the model. Dangerous actions always need
  a person, in every mode.
- `restart_service` only touches services on the machine owner's list. Protected process ids are never signalled.
- **Residual:** prompt injection cannot be eliminated, only contained. The allowlist, the owner's service list
  and human approval are what limit the damage. A malicious log line could still push the model to propose the wrong
  *allowed* action. `kill_process` can signal any process id the sudo rules permit (see below).

### The dashboard, or its operator, tries to give an agent more power than the machine owner intended
- Each machine sets `AERO_MAX_MODE` locally (default `observe`). The server can lower the mode, never raise it
  above that ceiling. Unknown modes behave as `observe`.
- An agent cannot approve its own action: the orchestrator ignores any report that moves an action out of
  "awaiting approval" except a human decision through the API. A decision is delivered only to the agent on
  that action's host, and only if it names the incident that agent is waiting on.

### Someone sends fake metrics or impersonates a host
- Every agent connection needs TLS and a per-host token (256 random bits, stored only as a SHA-256 hash).
  A batch whose host name differs from its token's host is rejected. Per-host rate limits apply.
- Agents authenticate to the server, and verify the server's certificate; plaintext requires an explicit opt-in flag.
- **Gap:** the orchestrator checks the token when a connection opens, not for its whole life. After rotating a
  host's token, an existing investigations connection survives until it reconnects. The metrics path checks
  every batch. Restart the agent or orchestrator to cut it off.

### A stolen or guessed dashboard login
- bcrypt passwords (12 to 72 bytes), login rate-limited per IP, identical timing for unknown emails, 12-hour
  random session tokens stored only as hashes, signed out when the tab closes (session storage).
- Two roles: viewers are read-only; only admins edit rules, change modes or approve actions, enforced on the server.
- **Gaps:** no multi-factor authentication, no password reset or change flow, no account lockout beyond the
  rate limit, no audit of failed logins. The per-IP limit is useless behind a reverse proxy (everyone shares an IP).

### Script injected into the web app steals the AI key
- The AI key lives in the browser only, in session storage unless the user ticks "remember", and goes only to the
  provider. Aero's servers never receive it; layout exports exclude it.
- A strict Content-Security-Policy (served by the API): scripts only from the app's own origin, no inline scripts,
  and requests only to the app, the Anthropic and OpenAI APIs and origins you list. Tested: a request to any other
  origin and an inline script are both blocked.
- Text is rendered as text, never as HTML.
- **Residual:** the key is readable by JavaScript in the page by design. A bug that lets an attacker run script
  from the app's own origin would expose it. Prefer a restricted, low-limit key.

### Tampering with the record of what happened
- Backend: the audit table is append-only (a database trigger blocks update, delete and truncate).
- Agent: a local hash-chained log; editing, reordering or deleting lines is detected (`audit-verify`), and the
  agent refuses to append to a log that does not verify. If it cannot write the log, it does not act.
- **Gap:** someone with root on the machine can delete or replace the whole local log, and someone with direct
  database superuser access can drop the trigger. Copy logs somewhere you do not control if you need more.

### A bad configuration exposes the system
- Services refuse to start without TLS unless an explicit development flag is set.
- Compose publishes only the web port and the two agent ports; the database and Kafka stay on the private
  network; services run unprivileged with read-only filesystems and no capabilities.
- Terraform refuses `0.0.0.0/0` for the dashboard unless forced, opens no SSH, requires IMDSv2 with hop limit 1,
  encrypts disks, and generates passwords on the server so they never enter Terraform state.

### Dependencies and images
- CI runs the tests, `govulncheck`, `npm audit`, an image scan, a Terraform scan and a secret scan, weekly as well
  as on every change, and Dependabot is configured. Our own image has no known fixable high or critical findings.
- **Gap:** the upstream database and Kafka images carry a few (one critical, a few dozen high at last check, in OS
  libraries and bundled runtimes) that we cannot patch. They are not reachable from outside the compose network.
- **Gap:** the C++ agent builds against the distribution's gRPC (1.51 on Ubuntu 24.04), which is older than
  the Go side's. Keep the OS updated.

## Not defended

- **A compromised stack host or database.** Whoever controls them controls the dashboard and can issue approvals.
- **Root on a monitored server.** They already own the machine; they can also read the agent's configuration,
  including the AI key.
- **A malicious admin.** Admins can approve any proposed fix.
- **The sudo rules are broad for `kill`.** sudoers cannot restrict which process id is signalled, so with sudo
  enabled the agent account can signal any process. `kill_process` always needs approval and protected
  ids are blocked, but remove those two sudoers lines if that is too much.
- **Unencrypted traffic inside the compose network** (to Postgres and Kafka) if you split the stack across machines.
- **Denial of service** beyond basic rate limits and size caps.
- **Multi-tenant isolation.** One stack serves one team; every admin and viewer sees every host.
- **Real-world scale and abuse testing.** The system has been exercised end to end on one machine; it has not
  been load-tested or reviewed by an outside party.
