terraform {
  required_providers {
    phron = {
      source = "phron-net/phron"
    }
  }
}

# Credentials come from PHRON_CLIENT_ID, PHRON_CLIENT_SECRET and
# PHRON_PROJECT_ID (all shown on the console's API clients page).
provider "phron" {}

data "phron_operating_systems" "all" {}

resource "phron_ssh_key" "admin" {
  name       = "admin"
  public_key = file("~/.ssh/id_ed25519.pub")
}

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

output "ssh_address" {
  value = phron_server.web.ssh_address
}
