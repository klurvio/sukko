variable "project_id" {
  description = "GCP project ID"
  type        = string
}

variable "region" {
  description = "GCP region for the registry"
  type        = string
}

variable "repository_id" {
  description = "Repository ID (name)"
  type        = string
  default     = "odin"
}

variable "description" {
  description = "Description of the repository"
  type        = string
  default     = "Container images for Odin WebSocket infrastructure"
}

variable "gke_service_accounts" {
  description = "List of GKE node service accounts that can pull images"
  type        = list(string)
  default     = []
}

variable "cicd_service_accounts" {
  description = "List of CI/CD service accounts that can push images"
  type        = list(string)
  default     = []
}
