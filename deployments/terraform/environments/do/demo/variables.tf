variable "cluster_name" {
  description = "Name of the DOKS cluster"
  type        = string
}

variable "region" {
  description = "DigitalOcean region"
  type        = string
  default     = "sfo3"
}

variable "kubernetes_version" {
  description = "Kubernetes version slug"
  type        = string
  default     = "1.35.1-do.0"
}

variable "node_pool_name" {
  description = "Name of the default node pool"
  type        = string
}

variable "node_size" {
  description = "Droplet size for worker nodes"
  type        = string
  default     = "s-2vcpu-4gb"
}

variable "node_count" {
  description = "Number of worker nodes"
  type        = number
  default     = 1
}
