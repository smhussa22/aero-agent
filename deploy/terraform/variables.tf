variable "region" {
  description = "AWS region."
  type        = string
  default     = "us-east-1"
}

variable "name" {
  description = "Name prefix for everything created."
  type        = string
  default     = "aero"
}

variable "admin_cidrs" {
  description = "Who may open the web app and API (port 8080), as CIDR blocks, for example [\"203.0.113.7/32\"]. No default on purpose."
  type        = list(string)
  validation {
    condition     = length(var.admin_cidrs) > 0 && alltrue([for c in var.admin_cidrs : can(cidrhost(c, 0))])
    error_message = "Give at least one valid CIDR block."
  }
}

variable "allow_open_admin" {
  description = "Set true to allow 0.0.0.0/0 in admin_cidrs. Leave false unless you really want the dashboard reachable from the whole internet."
  type        = bool
  default     = false
}

variable "agent_cidrs" {
  description = "Who may reach the agent ports (50051 metrics, 50052 investigations). Agents can be anywhere, and every connection needs TLS plus a per-host token, so the default is open. Narrow it if your servers have fixed addresses."
  type        = list(string)
  default     = ["0.0.0.0/0"]
}

variable "instance_type" {
  description = "Kafka, TimescaleDB and the services share this machine: use at least 8 GB of memory."
  type        = string
  default     = "t3.large"
}

variable "data_volume_gb" {
  description = "Size of the encrypted data disk (Docker images, the database and Kafka)."
  type        = number
  default     = 50
}

variable "backup_retention_days" {
  description = "Daily snapshots of the data disk are kept this many days."
  type        = number
  default     = 7
}

variable "tls_names" {
  description = "Extra DNS names to put in the certificate, for example [\"aero.example.com\"]. The Elastic IP is always included."
  type        = list(string)
  default     = []
}

variable "repo_url" {
  description = "Where the instance clones Aero Agent from."
  type        = string
  default     = "https://github.com/smhussa22/aero-agent.git"
}

variable "repo_ref" {
  description = "Branch or tag to deploy. Pin a tag in production so a push to main cannot change what a rebuilt server runs."
  type        = string
  default     = "main"
}

variable "vpc_cidr" {
  type    = string
  default = "10.42.0.0/16"
}

variable "enable_flow_logs" {
  description = "Record network flow logs for the VPC in CloudWatch (kept 30 days). Small cost, useful when investigating an incident."
  type        = bool
  default     = true
}

variable "public_url" {
  description = "Where browsers will reach the app, for example https://aero.example.com:8080. Register <public_url>/auth/callback with your identity provider."
  type        = string
}

variable "oidc_issuer" {
  description = "Issuer URL of your OpenID Connect provider (for WorkOS AuthKit, your AuthKit domain)."
  type        = string
}

variable "oidc_client_id" {
  type = string
}

variable "oidc_client_secret_parameter" {
  description = "Name of an SSM SecureString parameter you created holding the client secret, for example /aero/oidc-client-secret. It must sit under /<name>/ so the server may read it. Keeping the secret there keeps it out of Terraform state."
  type        = string
  default     = "/aero/oidc-client-secret"
}

variable "signup_open" {
  description = "true: anyone who signs in gets their own organization. false: only invited email addresses can sign in."
  type        = bool
  default     = false
}
