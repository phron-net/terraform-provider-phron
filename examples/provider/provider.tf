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
