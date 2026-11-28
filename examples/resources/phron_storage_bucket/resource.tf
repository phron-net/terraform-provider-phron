resource "phron_storage_bucket" "logs" {
  name     = "logs"
  location = "fsn1" # an id from data.phron_storage_overview.this.locations

  object_lock                        = true
  object_lock_default_retention_days = 30
  limit_bytes                        = 1000000000000
}
