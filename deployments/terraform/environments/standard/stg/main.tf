# =============================================================================
# GKE Standard - Staging Environment
# =============================================================================

module "gke" {
  source = "../../modules/gke-standard-cluster"

  # Project
  project_id = var.project_id
  region     = var.region
  zone       = var.zone

  # Cluster
  cluster_name = var.cluster_name
  environment  = var.environment
  network_name = var.network_name

  # Network CIDRs
  subnet_cidr   = var.subnet_cidr
  pods_cidr     = var.pods_cidr
  services_cidr = var.services_cidr

  # Node Pool
  node_machine_type  = var.node_machine_type
  node_count         = var.node_count
  node_disk_size_gb  = var.node_disk_size_gb
  use_spot_vms       = var.use_spot_vms
  taint_spot_nodes   = var.taint_spot_nodes
  enable_autoscaling = var.enable_autoscaling
  min_node_count     = var.min_node_count
  max_node_count     = var.max_node_count

  # Features
  enable_network_policy           = var.enable_network_policy
  enable_vertical_pod_autoscaling = var.enable_vertical_pod_autoscaling
  release_channel                 = var.release_channel
  deletion_protection             = var.deletion_protection

  # Note: Kernel tuning is done via DaemonSet in Helm chart
  # See docs/architecture/CONNECTION_LIMITS.md
}
