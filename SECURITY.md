# Security policy

## Reporting a vulnerability

Please report security problems **privately**, not in a public issue.

Use GitHub's private vulnerability reporting: on the repository page choose **Security**, then **Report a vulnerability**.
Include what you found, how to reproduce it, and what it lets an attacker do.

You can expect an acknowledgement and an honest assessment. This is a small project without a bug bounty or a
guaranteed response time.

## Scope

In scope: the code in this repository, the Docker and Terraform deployment files, and the documented configuration.
Out of scope: vulnerabilities in third-party software we depend on (report those upstream; tell us if our
configuration makes one worse), and anything listed under "Not defended" in `docs/threat-model.md`.

## What is and is not protected

`docs/threat-model.md` states what the system defends against, and lists known gaps, including ones you might
otherwise report. Start there.

## Supported versions

There are no releases yet. Only the `main` branch is maintained.
