# =============================================================================
# Project Configuration
# =============================================================================

variable "project_id" {
  description = "GCP project ID"
  type        = string
}

variable "region" {
  description = "GCP region"
  type        = string
  default     = "us-central1"
}

# =============================================================================
# VPC Configuration
# =============================================================================

variable "vpc_name" {
  description = "Shared VPC name"
  type        = string
}

# =============================================================================
# WS Cluster Subnet
# =============================================================================

variable "ws_subnet_cidr" {
  description = "WS cluster node subnet CIDR"
  type        = string
}

variable "ws_pods_cidr" {
  description = "WS cluster pods secondary CIDR"
  type        = string
}

variable "ws_services_cidr" {
  description = "WS cluster services secondary CIDR"
  type        = string
}
