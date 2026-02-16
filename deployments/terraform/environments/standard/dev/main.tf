# =============================================================================
# GKE Standard - Develop Environment
# =============================================================================

# Foundation layer provides VPC, subnets, and static IPs that persist
# across cluster destroy/recreate cycles.
data "terraform_remote_state" "foundation" {
  backend = "local"
  config = {
    path = "${path.module}/../dev-foundation/terraform.tfstate"
  }
}

module "gke" {
  source = "../../../modules/gke-standard-cluster"

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

  # Use foundation VPC instead of creating one
  external_vpc_id      = data.terraform_remote_state.foundation.outputs.vpc_id
  external_vpc_name    = data.terraform_remote_state.foundation.outputs.vpc_name
  external_subnet_id   = data.terraform_remote_state.foundation.outputs.ws_subnet_id
  external_subnet_name = data.terraform_remote_state.foundation.outputs.ws_subnet_name

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

  # Cloud NAT: increase ports for loadtest VMs (GCP default: 64 ports/VM)
  # Each outbound connection through NAT consumes one port. The loadtest VM
  # needs 10K+ simultaneous connections to the gateway LB, so 64 is insufficient.
  # Values must be powers of 2 when dynamic allocation is enabled.
  # Remove these lines to reset to GCP defaults (no cluster impact).
  nat_min_ports_per_vm               = 16384  # 2^14 — reserved upfront per VM
  nat_enable_dynamic_port_allocation = true   # scale beyond min up to max as needed
  nat_max_ports_per_vm               = 32768  # 2^15 — nearest power of 2 >= 30K target

  # Note: Kernel tuning is done via DaemonSet in Helm chart
  # See docs/architecture/CONNECTION_LIMITS.md
}
