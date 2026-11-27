terraform {
  required_providers {
    phron = {
      source = "phron-net/phron"
    }
  }
}

provider "phron" {}

resource "phron_storage_bucket" "logs" {
  name     = "logs"
  location = "fsn1" # an id from data.phron_storage_overview.this.locations

  object_lock                        = true
  object_lock_default_retention_days = 30
  limit_bytes                        = 1000000000000
}

resource "phron_storage_key" "writer" {
  name         = "log-writer"
  preset       = "read_write_objects"
  bucket_names = [phron_storage_bucket.logs.full_name]
  location     = "fsn1"
}

data "phron_storage_overview" "this" {
  depends_on = [phron_storage_bucket.logs]
  location   = "fsn1"
}

output "s3_endpoint" {
  value = data.phron_storage_overview.this.endpoint_url
}

output "access_key_id" {
  value = phron_storage_key.writer.access_key_id
}

# The secret is stored in the Terraform state: protect the state.
output "secret_access_key" {
  value     = phron_storage_key.writer.secret_access_key
  sensitive = true
}
