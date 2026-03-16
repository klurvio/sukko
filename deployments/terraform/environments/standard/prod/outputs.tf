# =============================================================================
# Outputs
# =============================================================================

output "project_id" {
  description = "The GCP project ID"
  value       = var.project_id
}

output "cluster_id" {
  description = "The ID of the GKE cluster"
  value       = module.gke.cluster_id
}

output "cluster_name" {
  description = "The name of the GKE cluster"
  value       = module.gke.cluster_name
}

output "cluster_endpoint" {
  description = "The endpoint of the GKE cluster"
  value       = module.gke.cluster_endpoint
  sensitive   = true
}

output "cluster_ca_certificate" {
  description = "The CA certificate of the GKE cluster"
  value       = module.gke.cluster_ca_certificate
  sensitive   = true
}

output "cluster_location" {
  description = "The location (zone) of the GKE cluster"
  value       = module.gke.cluster_location
}

output "network_name" {
  description = "The name of the VPC network"
  value       = module.gke.network_name
}

output "subnet_name" {
  description = "The name of the subnet"
  value       = module.gke.subnet_name
}

output "namespace" {
  description = "The Kubernetes namespace for odin"
  value       = var.namespace
}

output "node_pool_name" {
  description = "The name of the primary node pool"
  value       = module.gke.node_pool_name
}

output "spot_enabled" {
  description = "Whether Spot VMs are enabled"
  value       = var.use_spot_vms
}

output "autoscaling_enabled" {
  description = "Whether node autoscaling is enabled"
  value       = var.enable_autoscaling
}

output "kubeconfig_command" {
  description = "Command to configure kubectl"
  value       = "gcloud container clusters get-credentials ${module.gke.cluster_name} --zone ${var.zone} --project ${var.project_id}"
}
