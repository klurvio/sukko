# =============================================================================
# Outputs
# =============================================================================

output "vpc_name" {
  description = "The name of the shared VPC"
  value       = google_compute_network.vpc.name
}

output "vpc_id" {
  description = "The ID of the shared VPC"
  value       = google_compute_network.vpc.id
}

output "ws_subnet_name" {
  description = "The name of the WS cluster subnet"
  value       = google_compute_subnetwork.ws.name
}

output "ws_subnet_id" {
  description = "The ID of the WS cluster subnet"
  value       = google_compute_subnetwork.ws.id
}

output "gateway_external_ip" {
  description = "The static external IP for the gateway LoadBalancer"
  value       = google_compute_address.gateway_external.address
}

output "redpanda_internal_ip" {
  description = "The static internal IP for the Redpanda LoadBalancer"
  value       = google_compute_address.redpanda_internal.address
}
