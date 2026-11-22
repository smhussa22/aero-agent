# Aero Agent on AWS

One encrypted EC2 instance running the Docker Compose stack from `deploy/docker`, with a fixed IP, daily disk
snapshots, and shell access through Session Manager (no SSH, port 22 stays closed).

Why one server and not RDS + MSK: RDS has no TimescaleDB, and managed Kafka costs far more than this project
is worth to a small team. A single hardened host with a snapshotted disk is the honest fit. If you outgrow it,
split the stack, and add TLS between the pieces.

## Use

```sh
cd deploy/terraform
terraform init
# 1. at your identity provider (for example WorkOS AuthKit) create an application whose redirect URL is
#    <public_url>/auth/callback, then store its client secret where the server can read it, outside Terraform:
aws ssm put-parameter --name /aero/oidc-client-secret --type SecureString --value '<client secret>'

# 2. apply; admin_cidrs should be your own IP, so only you can open the dashboard
terraform apply -var 'admin_cidrs=["203.0.113.7/32"]' -var 'public_url=https://203.0.113.7:8080'   -var 'oidc_issuer=https://<your AuthKit domain>' -var 'oidc_client_id=<client id>'
```

`admin_cidrs` has no default and refuses `0.0.0.0/0` unless you also set `allow_open_admin = true`.
Pin `repo_ref` to a release tag for production.

The first boot takes several minutes (it builds the images). Watch it with the `shell` output, then
`tail -f /var/log/aero-bootstrap.log`. Then, from that shell, create the first admin (`first_admin` output) and register
servers with `hostctl add` as in `deploy/README.md`. Fetch the certificate agents should trust with the
`certificate_command` output.

## What it creates

VPC, one public subnet, an Elastic IP, a security group (8080 for `admin_cidrs`; 50051 and 50052 for
`agent_cidrs`), an instance role limited to Session Manager and its own `/aero/*` parameters, an encrypted
root and data disk, a daily snapshot policy, and VPC flow logs (kept 30 days).

## Security notes

- Database and Kafka passwords are generated **on the server** at first boot and are never in Terraform
  state. The certificate is self-signed for the Elastic IP; put a real one in `/opt/aero/repo/deploy/docker/certs`
  and `docker compose up -d` to replace it.
- IMDSv2 only, with hop limit 1, so containers cannot read the instance role's credentials.
- Outbound traffic is limited to ports 80 and 443. It cannot be limited to fixed addresses, because package
  mirrors, Docker Hub and GitHub have none. The scanner flags this; the suppression is commented in `main.tf`.
- Upgrades: `sudo git -C /opt/aero/repo pull && cd /opt/aero/repo/deploy/docker && sudo docker compose up -d --build`.
  A changed `repo_ref` does not replace the server.
- Cost: roughly a `t3.large`, 70 GB of disk and one Elastic IP. Destroy with `terraform destroy`; the snapshots
  are removed by their policy, not by destroy, so delete them yourself if you want them gone.

Checked with `terraform validate`, `terraform fmt` and `trivy config` (no findings beyond the documented suppressions).
It has not been applied to a real AWS account.
