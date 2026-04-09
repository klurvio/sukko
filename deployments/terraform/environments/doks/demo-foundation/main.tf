resource "digitalocean_reserved_ip" "gateway" {
  region = var.region
}

resource "digitalocean_reserved_ip" "provisioning" {
  region = var.region
}
