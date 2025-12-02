variable "project_id" {
  description = "GCP project ID"
  type        = string
}

variable "region" {
  description = "GCP region for the cluster"
  type        = string
}

variable "cluster_name" {
  description = "Name of the GKE cluster"
  type        = string
}

variable "network_id" {
  description = "VPC network ID"
  type        = string
}

variable "subnet_id" {
  description = "Subnet ID"
  type        = string
}

variable "release_channel" {
  description = "GKE release channel (RAPID, REGULAR, STABLE)"
  type        = string
  default     = "REGULAR"
}

variable "enable_private_nodes" {
  description = "Enable private nodes (no public IPs)"
  type        = bool
  default     = false
}

variable "master_cidr" {
  description = "CIDR range for the master network (required for private clusters)"
  type        = string
  default     = ""
}

variable "deletion_protection" {
  description = "Enable deletion protection for the cluster"
  type        = bool
  default     = false
}
