resource "phron_storage_bucket_limit" "logs" {
  bucket_name = phron_storage_bucket.logs.full_name
  limit_bytes = 1000000000000
  location    = "fsn1"
}
