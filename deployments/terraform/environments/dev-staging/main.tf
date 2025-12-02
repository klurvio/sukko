module "vpc" {
  source       = "../../modules/vpc"
  project_id   = var.gcp_project
  region       = var.gcp_region
  network_name = "odin-dev-staging"
  subnet_cidr  = "10.0.0.0/20"
  pods_cidr    = "10.4.0.0/14"
  services_cidr = "10.8.0.0/20"
}

module "gke" {
  source       = "../../modules/gke-cluster"
  project_id   = var.gcp_project
  region       = var.gcp_region
  cluster_name = "odin-dev-staging"

  network_id = module.vpc.network_id
  subnet_id  = module.vpc.subnet_id

  release_channel      = "REGULAR"
  enable_private_nodes = false
  deletion_protection  = false
}

resource "kubernetes_namespace" "dev" {
  metadata {
    name = "odin-dev"
    labels = {
      environment = "develop"
      managed-by  = "terraform"
    }
  }

  depends_on = [module.gke]
}

resource "kubernetes_namespace" "staging" {
  metadata {
    name = "odin-staging"
    labels = {
      environment = "staging"
      managed-by  = "terraform"
    }
  }

  depends_on = [module.gke]
}
