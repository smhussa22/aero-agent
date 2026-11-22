# One EC2 instance running the same Docker Compose stack as deploy/docker. TimescaleDB is not offered by
# RDS and managed Kafka costs far more than this project is worth to a small team, so a single well-secured
# host with an encrypted, snapshotted data disk is the honest fit. Secrets are generated on the instance at
# first boot and never pass through Terraform, so they are not in the state file.

data "aws_availability_zones" "available" {
  state = "available"
}

data "aws_ami" "ubuntu" {
  most_recent = true
  owners      = ["099720109477"] # Canonical
  filter {
    name   = "name"
    values = ["ubuntu/images/hvm-ssd-gp3/ubuntu-noble-24.04-amd64-server-*"]
  }
}

# ---------- network ----------

resource "aws_vpc" "this" {
  cidr_block           = var.vpc_cidr
  enable_dns_hostnames = true
  tags                 = { Name = var.name }
}

resource "aws_internet_gateway" "this" {
  vpc_id = aws_vpc.this.id
  tags   = { Name = var.name }
}

resource "aws_subnet" "public" {
  vpc_id                  = aws_vpc.this.id
  cidr_block              = cidrsubnet(var.vpc_cidr, 8, 0)
  availability_zone       = data.aws_availability_zones.available.names[0]
  map_public_ip_on_launch = false # the Elastic IP below is the only public address
  tags                    = { Name = "${var.name}-public" }
}

resource "aws_route_table" "public" {
  vpc_id = aws_vpc.this.id
  route {
    cidr_block = "0.0.0.0/0"
    gateway_id = aws_internet_gateway.this.id
  }
  tags = { Name = var.name }
}

resource "aws_route_table_association" "public" {
  subnet_id      = aws_subnet.public.id
  route_table_id = aws_route_table.public.id
}

# Lock the default security group so nothing can use it by accident.
resource "aws_default_security_group" "default" {
  vpc_id = aws_vpc.this.id
}

resource "aws_security_group" "this" {
  name_prefix = "${var.name}-"
  description = "Aero Agent: web app for admins, agent ports for monitored servers. No SSH."
  vpc_id      = aws_vpc.this.id

  lifecycle {
    create_before_destroy = true
    precondition {
      condition     = var.allow_open_admin || !contains(var.admin_cidrs, "0.0.0.0/0")
      error_message = "admin_cidrs contains 0.0.0.0/0, which exposes the dashboard to the whole internet. Narrow it, or set allow_open_admin = true if you mean it."
    }
  }
}

resource "aws_vpc_security_group_ingress_rule" "web" {
  for_each          = toset(var.admin_cidrs)
  security_group_id = aws_security_group.this.id
  description       = "Web app and API (admins)"
  from_port         = 8080
  to_port           = 8080
  ip_protocol       = "tcp"
  cidr_ipv4         = each.value
}

resource "aws_vpc_security_group_ingress_rule" "ingest" {
  for_each          = toset(var.agent_cidrs)
  security_group_id = aws_security_group.this.id
  description       = "Metrics from monitoring agents (TLS + token)"
  from_port         = 50051
  to_port           = 50051
  ip_protocol       = "tcp"
  cidr_ipv4         = each.value
}

resource "aws_vpc_security_group_ingress_rule" "orchestrator" {
  for_each          = toset(var.agent_cidrs)
  security_group_id = aws_security_group.this.id
  description       = "Remediation agents (TLS + token)"
  from_port         = 50052
  to_port           = 50052
  ip_protocol       = "tcp"
  cidr_ipv4         = each.value
}

# The server pulls images, clones the repo and talks to AWS Systems Manager, all over HTTPS.
# Egress cannot be narrowed to fixed addresses (Docker Hub, GitHub and package mirrors have none), so it is
# limited to the two ports instead.
#trivy:ignore:AVD-AWS-0104
resource "aws_vpc_security_group_egress_rule" "https" {
  security_group_id = aws_security_group.this.id
  description       = "HTTPS out"
  from_port         = 443
  to_port           = 443
  ip_protocol       = "tcp"
  cidr_ipv4         = "0.0.0.0/0"
}

#trivy:ignore:AVD-AWS-0104
resource "aws_vpc_security_group_egress_rule" "http" {
  security_group_id = aws_security_group.this.id
  description       = "Package repositories"
  from_port         = 80
  to_port           = 80
  ip_protocol       = "tcp"
  cidr_ipv4         = "0.0.0.0/0"
}

# ---------- identity: Session Manager for shell access, and the right to publish its own public certificate ----------

data "aws_iam_policy_document" "assume" {
  statement {
    actions = ["sts:AssumeRole"]
    principals {
      type        = "Service"
      identifiers = ["ec2.amazonaws.com"]
    }
  }
}

data "aws_caller_identity" "current" {}

resource "aws_iam_role" "this" {
  name_prefix        = "${var.name}-"
  assume_role_policy = data.aws_iam_policy_document.assume.json
}

resource "aws_iam_role_policy_attachment" "ssm" {
  role       = aws_iam_role.this.name
  policy_arn = "arn:aws:iam::aws:policy/AmazonSSMManagedInstanceCore"
}

data "aws_iam_policy_document" "own_parameters" {
  statement {
    actions   = ["ssm:PutParameter", "ssm:GetParameter"]
    resources = ["arn:aws:ssm:${var.region}:${data.aws_caller_identity.current.account_id}:parameter/${var.name}/*"]
  }
}

resource "aws_iam_role_policy" "own_parameters" {
  name   = "own-parameters"
  role   = aws_iam_role.this.id
  policy = data.aws_iam_policy_document.own_parameters.json
}

resource "aws_iam_instance_profile" "this" {
  name_prefix = "${var.name}-"
  role        = aws_iam_role.this.name
}

# ---------- storage ----------

# Encrypted with the AWS-managed EBS key. A customer-managed key adds cost and a way to lock yourself out,
# which a one-server deployment does not need.
#trivy:ignore:AVD-AWS-0027
resource "aws_ebs_volume" "data" {
  availability_zone = aws_subnet.public.availability_zone
  size              = var.data_volume_gb
  type              = "gp3"
  encrypted         = true
  tags              = { Name = "${var.name}-data", Backup = var.name }
}

resource "aws_volume_attachment" "data" {
  device_name = "/dev/sdf"
  volume_id   = aws_ebs_volume.data.id
  instance_id = aws_instance.this.id
}

# ---------- the server ----------

resource "aws_eip" "this" {
  domain = "vpc"
  tags   = { Name = var.name }
}

resource "aws_instance" "this" {
  ami                    = data.aws_ami.ubuntu.id
  instance_type          = var.instance_type
  subnet_id              = aws_subnet.public.id
  vpc_security_group_ids = [aws_security_group.this.id]
  iam_instance_profile   = aws_iam_instance_profile.this.name
  ebs_optimized          = true

  # a changed repo_ref must not replace the server and its data; upgrade from a shell with git pull
  user_data_replace_on_change = false
  user_data = templatefile("${path.module}/userdata.sh.tftpl", {
    region            = var.region
    name              = var.name
    repo_url          = var.repo_url
    repo_ref          = var.repo_ref
    public_ip         = aws_eip.this.public_ip
    tls_names         = join(" ", var.tls_names)
    public_url        = var.public_url
    oidc_issuer       = var.oidc_issuer
    oidc_client_id    = var.oidc_client_id
    oidc_secret_param = var.oidc_client_secret_parameter
    signup_open       = var.signup_open ? "1" : "0"
    volume_dev        = "/dev/disk/by-id/nvme-Amazon_Elastic_Block_Store_${replace(aws_ebs_volume.data.id, "-", "")}"
  })

  metadata_options {
    http_endpoint               = "enabled"
    http_tokens                 = "required" # IMDSv2 only
    http_put_response_hop_limit = 1          # containers cannot reach the instance role's credentials
  }

  root_block_device {
    volume_type = "gp3"
    volume_size = 20
    encrypted   = true
  }

  tags = { Name = var.name }

  lifecycle {
    ignore_changes = [ami, user_data] # an AMI refresh or a repo_ref edit never rebuilds a running server
  }
}

resource "aws_eip_association" "this" {
  instance_id   = aws_instance.this.id
  allocation_id = aws_eip.this.id
}

# ---------- daily snapshots of the data disk ----------

data "aws_iam_policy_document" "dlm_assume" {
  statement {
    actions = ["sts:AssumeRole"]
    principals {
      type        = "Service"
      identifiers = ["dlm.amazonaws.com"]
    }
  }
}

resource "aws_iam_role" "dlm" {
  name_prefix        = "${var.name}-dlm-"
  assume_role_policy = data.aws_iam_policy_document.dlm_assume.json
}

resource "aws_iam_role_policy_attachment" "dlm" {
  role       = aws_iam_role.dlm.name
  policy_arn = "arn:aws:iam::aws:policy/service-role/AWSDataLifecycleManagerServiceRole"
}

resource "aws_dlm_lifecycle_policy" "data" {
  description        = "Daily snapshots of the Aero Agent data disk"
  execution_role_arn = aws_iam_role.dlm.arn
  state              = "ENABLED"

  policy_details {
    resource_types = ["VOLUME"]
    target_tags    = { Backup = var.name }
    schedule {
      name = "daily"
      create_rule {
        interval      = 24
        interval_unit = "HOURS"
        times         = ["03:00"]
      }
      retain_rule {
        count = var.backup_retention_days
      }
      copy_tags = true
    }
  }
}

# ---------- network visibility: who talked to the server, for investigating an incident ----------

#trivy:ignore:AVD-AWS-0017
resource "aws_cloudwatch_log_group" "flow" {
  count             = var.enable_flow_logs ? 1 : 0
  name              = "/${var.name}/vpc-flow"
  retention_in_days = 30
}

data "aws_iam_policy_document" "flow_assume" {
  statement {
    actions = ["sts:AssumeRole"]
    principals {
      type        = "Service"
      identifiers = ["vpc-flow-logs.amazonaws.com"]
    }
  }
}

resource "aws_iam_role" "flow" {
  count              = var.enable_flow_logs ? 1 : 0
  name_prefix        = "${var.name}-flow-"
  assume_role_policy = data.aws_iam_policy_document.flow_assume.json
}

data "aws_iam_policy_document" "flow_write" {
  count = var.enable_flow_logs ? 1 : 0
  statement {
    actions   = ["logs:CreateLogStream", "logs:PutLogEvents", "logs:DescribeLogStreams"]
    resources = ["${aws_cloudwatch_log_group.flow[0].arn}:*"]
  }
}

resource "aws_iam_role_policy" "flow" {
  count  = var.enable_flow_logs ? 1 : 0
  name   = "write-flow-logs"
  role   = aws_iam_role.flow[0].id
  policy = data.aws_iam_policy_document.flow_write[0].json
}

resource "aws_flow_log" "this" {
  count           = var.enable_flow_logs ? 1 : 0
  vpc_id          = aws_vpc.this.id
  traffic_type    = "ALL"
  log_destination = aws_cloudwatch_log_group.flow[0].arn
  iam_role_arn    = aws_iam_role.flow[0].arn
}
