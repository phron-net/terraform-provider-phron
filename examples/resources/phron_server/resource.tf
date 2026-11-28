resource "phron_server" "web" {
  name                = "web-1"
  location            = "fsn1" # an id from data.phron_locations
  operating_system_id = data.phron_operating_systems.all.operating_systems[0].id
  ssh_key_ids         = [phron_ssh_key.admin.id]

  tier       = "shared"
  vcpus      = 2
  memory_gib = 4
  disk_gib   = 40

  user_data = <<-EOT
    #cloud-config
    packages:
      - nginx
  EOT
}
