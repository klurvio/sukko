output "cluster_id" {
  description = "ID of the DOKS cluster"
  value       = module.doks.cluster_id
}

output "cluster_name" {
  description = "Name of the DOKS cluster"
  value       = module.doks.cluster_name
}

output "cluster_endpoint" {
  description = "API server endpoint URL"
  value       = module.doks.cluster_endpoint
  sensitive   = true
}

output "kubeconfig_command" {
  description = "Command to configure kubectl"
  value       = "doctl kubernetes cluster kubeconfig save ${var.cluster_name}"
}
