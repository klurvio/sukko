variable "cluster_name" {
  description = "Name of the DOKS cluster"
  type        = string
}

variable "region" {
  description = "DigitalOcean region for the cluster"
  type        = string
  default     = "sfo3"
}

variable "kubernetes_version" {
  description = "Kubernetes version slug (e.g., 1.35.1-do.0)"
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
  default     = "s-2vcpu-2gb"
}

variable "node_count" {
  description = "Number of worker nodes"
  type        = number
  default     = 1
}

variable "surge_upgrade" {
  description = "Enable surge upgrade (temporary extra node during K8s upgrades)"
  type        = bool
  default     = true
}

variable "auto_upgrade" {
  description = "Enable automatic K8s version upgrades"
  type        = bool
  default     = false
}

variable "maintenance_start_time" {
  description = "Maintenance window start time (UTC, HH:MM format)"
  type        = string
  default     = "17:00"
}

variable "maintenance_day" {
  description = "Day of week for maintenance window (any, monday, tuesday, etc.)"
  type        = string
  default     = "any"
}

variable "destroy_all_associated_resources" {
  description = "Destroy LBs and volumes when cluster is destroyed"
  type        = bool
  default     = true
}
