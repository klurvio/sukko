module "vpc" {
  source        = "../../modules/vpc"
  project_id    = var.gcp_project
  region        = var.gcp_region
  network_name  = "odin-production"
  subnet_cidr   = "10.16.0.0/20"
  pods_cidr     = "10.20.0.0/14"
  services_cidr = "10.24.0.0/20"
}

module "gke" {
  source       = "../../modules/gke-cluster"
  project_id   = var.gcp_project
  region       = var.gcp_region
  cluster_name = "odin-production"

  network_id = module.vpc.network_id
  subnet_id  = module.vpc.subnet_id

  release_channel      = "STABLE"
  enable_private_nodes = true
  master_cidr          = "172.16.0.0/28"
  deletion_protection  = true
}

resource "kubernetes_namespace" "prod" {
  metadata {
    name = "odin-prod"
    labels = {
      environment = "production"
      managed-by  = "terraform"
    }
  }

  depends_on = [module.gke]
}
