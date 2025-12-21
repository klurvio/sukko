terraform {
  required_version = ">= 1.6.0"

  cloud {
    organization = "toniq"
    workspaces {
      name = "odin-production"
    }
  }

  required_providers {
    google = {
      source  = "hashicorp/google"
      version = "~> 5.0"
    }
    kubernetes = {
      source  = "hashicorp/kubernetes"
      version = "~> 2.23"
    }
  }
}

provider "google" {
  project = var.gcp_project
  region  = var.gcp_region
}

# Configure Kubernetes provider to use GKE cluster
data "google_client_config" "default" {}

provider "kubernetes" {
  host                   = "https://${module.gke.cluster_endpoint}"
  token                  = data.google_client_config.default.access_token
  cluster_ca_certificate = base64decode(module.gke.cluster_ca_certificate)
}
