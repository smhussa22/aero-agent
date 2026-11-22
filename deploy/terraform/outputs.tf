output "public_ip" {
  description = "The server's fixed public address."
  value       = aws_eip.this.public_ip
}

output "web_url" {
  description = "The dashboard. The certificate is self-signed until you install your own, so the browser warns once."
  value       = "https://${aws_eip.this.public_ip}:8080"
}

output "ingest_address" {
  description = "Where monitoring agents send metrics (AERO_TOKEN, AERO_CA_CERT)."
  value       = "${aws_eip.this.public_ip}:50051"
}

output "orchestrator_address" {
  description = "Where remediation agents connect (AERO_ORCH_ADDR)."
  value       = "${aws_eip.this.public_ip}:50052"
}

output "shell" {
  description = "Open a shell on the server (no SSH, no open port 22). Needs the AWS CLI and its Session Manager plugin."
  value       = "aws ssm start-session --region ${var.region} --target ${aws_instance.this.id}"
}

output "certificate_command" {
  description = "Fetch the public certificate so agents can trust the server (AERO_CA_CERT)."
  value       = "aws ssm get-parameter --region ${var.region} --name /${var.name}/server-cert --query Parameter.Value --output text > server.crt"
}

output "first_admin" {
  description = "After the server finishes booting (a few minutes), invite the first admin from a shell on it, then sign in with that email address."
  value       = "cd /opt/aero/repo/deploy/docker && sudo docker compose run --rm tools userctl invite you@example.com admin --new-org 'Your team'"
}
