module "doks" {
  source = "../../../modules/doks-cluster"

  cluster_name       = var.cluster_name
  region             = var.region
  kubernetes_version = var.kubernetes_version
  node_pool_name     = var.node_pool_name
  node_size          = var.node_size
  node_count         = var.node_count

  # Remaining module variables (surge_upgrade, auto_upgrade, maintenance_*,
  # destroy_all_associated_resources) use module defaults.
}
