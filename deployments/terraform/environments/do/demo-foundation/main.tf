resource "digitalocean_reserved_ip" "gateway" {
  region = var.region
}
