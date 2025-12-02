variable "gcp_project" {
  description = "GCP project ID"
  type        = string
}

variable "gcp_region" {
  description = "GCP region"
  type        = string
  default     = "us-central1"
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
