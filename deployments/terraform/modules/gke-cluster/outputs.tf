output "cluster_id" {
  description = "The ID of the GKE cluster"
  value       = google_container_cluster.autopilot.id
}

output "cluster_name" {
  description = "The name of the GKE cluster"
  value       = google_container_cluster.autopilot.name
}

output "cluster_endpoint" {
  description = "The endpoint of the GKE cluster"
  value       = google_container_cluster.autopilot.endpoint
  sensitive   = true
}

output "cluster_ca_certificate" {
  description = "The CA certificate of the GKE cluster"
  value       = google_container_cluster.autopilot.master_auth[0].cluster_ca_certificate
  sensitive   = true
}

output "cluster_location" {
  description = "The location of the GKE cluster"
  value       = google_container_cluster.autopilot.location
}

output "kubeconfig" {
  description = "Kubeconfig details for connecting to the cluster"
  value = {
    host                   = "https://${google_container_cluster.autopilot.endpoint}"
    cluster_ca_certificate = google_container_cluster.autopilot.master_auth[0].cluster_ca_certificate
  }
  sensitive = true
}
