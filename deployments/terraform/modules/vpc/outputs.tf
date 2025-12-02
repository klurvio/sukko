output "network_id" {
  description = "The ID of the VPC network"
  value       = google_compute_network.vpc.id
}

output "network_name" {
  description = "The name of the VPC network"
  value       = google_compute_network.vpc.name
}

output "network_self_link" {
  description = "The self link of the VPC network"
  value       = google_compute_network.vpc.self_link
}

output "subnet_id" {
  description = "The ID of the subnet"
  value       = google_compute_subnetwork.subnet.id
}

output "subnet_name" {
  description = "The name of the subnet"
  value       = google_compute_subnetwork.subnet.name
}

output "subnet_self_link" {
  description = "The self link of the subnet"
  value       = google_compute_subnetwork.subnet.self_link
}
