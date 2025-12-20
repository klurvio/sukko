# =============================================================================
# Outputs
# =============================================================================

output "project_id" {
  description = "The GCP project ID"
  value       = var.project_id
}

output "cluster_id" {
  description = "The ID of the GKE cluster"
  value       = google_container_cluster.primary.id
}

output "cluster_name" {
  description = "The name of the GKE cluster"
  value       = google_container_cluster.primary.name
}

output "cluster_endpoint" {
  description = "The endpoint of the GKE cluster"
  value       = google_container_cluster.primary.endpoint
  sensitive   = true
}

output "cluster_ca_certificate" {
  description = "The CA certificate of the GKE cluster"
  value       = google_container_cluster.primary.master_auth[0].cluster_ca_certificate
  sensitive   = true
}

output "cluster_location" {
  description = "The location (zone) of the GKE cluster"
  value       = google_container_cluster.primary.location
}

output "network_name" {
  description = "The name of the VPC network"
  value       = google_compute_network.vpc.name
}

output "subnet_name" {
  description = "The name of the subnet"
  value       = google_compute_subnetwork.subnet.name
}

output "namespace" {
  description = "The Kubernetes namespace for odin"
  value       = var.namespace
}

output "node_pool_name" {
  description = "The name of the primary node pool"
  value       = google_container_node_pool.primary.name
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
  value       = "gcloud container clusters get-credentials ${google_container_cluster.primary.name} --zone ${var.zone} --project ${var.project_id}"
}
