output "gateway_external_ip" {
  description = "Reserved IP for the gateway LoadBalancer"
  value       = digitalocean_reserved_ip.gateway.ip_address
}
