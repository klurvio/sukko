output "cluster_id" {
  description = "ID of the DOKS cluster"
  value       = digitalocean_kubernetes_cluster.this.id
}

output "cluster_name" {
  description = "Name of the DOKS cluster"
  value       = digitalocean_kubernetes_cluster.this.name
}

output "cluster_endpoint" {
  description = "API server endpoint URL"
  value       = digitalocean_kubernetes_cluster.this.endpoint
  sensitive   = true
}

output "cluster_kubeconfig" {
  description = "Raw kubeconfig for CI/CD"
  value       = digitalocean_kubernetes_cluster.this.kube_config[0].raw_config
  sensitive   = true
}

output "cluster_urn" {
  description = "DigitalOcean resource URN"
  value       = digitalocean_kubernetes_cluster.this.urn
}
