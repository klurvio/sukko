resource "digitalocean_kubernetes_cluster" "this" {
  name    = var.cluster_name
  region  = var.region
  version = var.kubernetes_version

  node_pool {
    name       = var.node_pool_name
    size       = var.node_size
    node_count = var.node_count
  }

  surge_upgrade = var.surge_upgrade
  auto_upgrade  = var.auto_upgrade

  maintenance_policy {
    start_time = var.maintenance_start_time
    day        = var.maintenance_day
  }

  destroy_all_associated_resources = var.destroy_all_associated_resources
}
