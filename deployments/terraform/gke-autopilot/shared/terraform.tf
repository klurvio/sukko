terraform {
  required_version = ">= 1.6.0"

  cloud {
    organization = "toniq"
    workspaces {
      name = "odin-shared"
    }
  }

  required_providers {
    google = {
      source  = "hashicorp/google"
      version = "~> 5.0"
    }
  }
}

provider "google" {
  project = var.gcp_project
  region  = var.gcp_region
}
