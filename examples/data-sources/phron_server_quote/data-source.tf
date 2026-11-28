data "phron_server_quote" "web" {
  vcpus      = 2
  memory_gib = 4
  disk_gib   = 40
  tier       = "shared"
  location   = "fsn1"
}
