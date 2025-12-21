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
  description = "The location of the GKE cluster"
  value       = module.gke.cluster_location
}

output "network_name" {
  description = "The name of the VPC network"
  value       = module.vpc.network_name
}

output "subnet_name" {
  description = "The name of the subnet"
  value       = module.vpc.subnet_name
}

output "dev_namespace" {
  description = "The develop namespace"
  value       = kubernetes_namespace.dev.metadata[0].name
}

output "staging_namespace" {
  description = "The staging namespace"
  value       = kubernetes_namespace.staging.metadata[0].name
}
