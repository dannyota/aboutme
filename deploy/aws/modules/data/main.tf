# RDS PostgreSQL and the private media bucket.

resource "aws_db_subnet_group" "main" {
  name       = var.name
  subnet_ids = var.private_subnet_ids
}

resource "aws_db_parameter_group" "main" {
  name   = var.name
  family = "postgres18"

  # RDS records this parameter as applying at reboot.
  parameter {
    name         = "rds.force_ssl"
    value        = "1"
    apply_method = "pending-reboot"
  }

  # Keeps SQL text, including role changes, out of the logs.
  parameter {
    name  = "log_statement"
    value = "none"
  }
}

resource "aws_db_instance" "main" {
  identifier                  = var.name
  engine                      = "postgres"
  engine_version              = "18.6"
  instance_class              = "db.t4g.micro"
  allocated_storage           = 20
  storage_type                = "gp3"
  storage_encrypted           = true
  db_name                     = "aboutme"
  username                    = "aboutme"
  manage_master_user_password = true
  db_subnet_group_name        = aws_db_subnet_group.main.name
  vpc_security_group_ids      = [var.db_security_group_id]
  parameter_group_name        = aws_db_parameter_group.main.name
  publicly_accessible         = false
  multi_az                    = false
  backup_retention_period     = 30
  backup_window               = "18:00-18:30"
  maintenance_window          = "sun:19:00-sun:19:30"
  copy_tags_to_snapshot       = true
  deletion_protection         = true
  skip_final_snapshot         = false
  final_snapshot_identifier   = "${var.name}-final"
  auto_minor_version_upgrade  = true
  ca_cert_identifier          = "rds-ca-rsa2048-g1"
  apply_immediately           = false
}

resource "aws_s3_bucket" "media" {
  bucket = var.media_bucket_name
}

resource "aws_s3_bucket_public_access_block" "media" {
  bucket                  = aws_s3_bucket.media.id
  block_public_acls       = true
  block_public_policy     = true
  ignore_public_acls      = true
  restrict_public_buckets = true
}

resource "aws_s3_bucket_ownership_controls" "media" {
  bucket = aws_s3_bucket.media.id
  rule {
    object_ownership = "BucketOwnerEnforced"
  }
}

resource "aws_s3_bucket_server_side_encryption_configuration" "media" {
  bucket = aws_s3_bucket.media.id
  rule {
    apply_server_side_encryption_by_default {
      sse_algorithm = "AES256"
    }
  }
}

# No versioning: a deleted media object must be gone, per the deployment design.
resource "aws_s3_bucket_lifecycle_configuration" "media" {
  bucket = aws_s3_bucket.media.id
  rule {
    id     = "abort-incomplete-uploads"
    status = "Enabled"
    filter {}
    abort_incomplete_multipart_upload {
      days_after_initiation = 1
    }
  }
}

resource "aws_s3_bucket_policy" "media" {
  bucket = aws_s3_bucket.media.id
  policy = jsonencode({
    Version = "2012-10-17"
    Statement = [{
      Sid       = "DenyInsecureTransport"
      Effect    = "Deny"
      Principal = "*"
      Action    = "s3:*"
      Resource  = [aws_s3_bucket.media.arn, "${aws_s3_bucket.media.arn}/*"]
      Condition = { Bool = { "aws:SecureTransport" = "false" } }
    }]
  })
  depends_on = [aws_s3_bucket_public_access_block.media]
}

# Production minimum-release fence and deployment operation lock. OpenTofu
# owns only the table; the deploy role manages the singleton "application"
# item at runtime, per docs/design/passkey-release-fence.md. No
# server_side_encryption block means AWS-owned keys, the encryption this
# table uses.
resource "aws_dynamodb_table" "release_fence" {
  name         = "${var.name}-release-fence"
  billing_mode = "PAY_PER_REQUEST"
  hash_key     = "id"

  attribute {
    name = "id"
    type = "S"
  }

  point_in_time_recovery {
    enabled = true
  }

  deletion_protection_enabled = true

  lifecycle {
    prevent_destroy = true
  }
}
