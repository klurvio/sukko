# =============================================================================
# Outputs
# =============================================================================

output "vpc_name" {
  description = "The name of the shared VPC"
  value       = module.foundation.vpc_name
}

output "vpc_id" {
  description = "The ID of the shared VPC"
  value       = module.foundation.vpc_id
}

output "ws_subnet_name" {
  description = "The name of the WS cluster subnet"
  value       = module.foundation.ws_subnet_name
}

output "ws_subnet_id" {
  description = "The ID of the WS cluster subnet"
  value       = module.foundation.ws_subnet_id
}

output "gateway_external_ip" {
  description = "The static external IP for the gateway LoadBalancer"
  value       = module.foundation.gateway_external_ip
}

output "redpanda_internal_ip" {
  description = "The static internal IP for the Redpanda LoadBalancer"
  value       = module.foundation.redpanda_internal_ip
}
