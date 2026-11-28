resource "phron_storage_key" "writer" {
  name         = "log-writer"
  preset       = "read_write_objects"
  bucket_names = [phron_storage_bucket.logs.full_name]
  location     = "fsn1"
}
