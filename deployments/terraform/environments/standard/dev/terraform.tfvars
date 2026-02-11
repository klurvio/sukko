# =============================================================================
# GKE Standard - Dev Environment
# =============================================================================

# Project Configuration
project_id = "odin-9e902"

# Region & Zone
region = "us-central1"
zone   = "us-central1-a"

# Cluster Configuration
cluster_name = "odin-ws-dev"
environment  = "dev"
namespace    = "odin-ws-dev"
network_name = "odin-ws-dev-vpc"

# Node Pool Configuration
node_machine_type = "e2-standard-4"
node_disk_size_gb = 50

# Spot VMs - 60-90% cheaper
use_spot_vms     = true
taint_spot_nodes = false

# Scaling - Single node for POC
node_count         = 1
enable_autoscaling = false

# Features
enable_network_policy           = true
enable_vertical_pod_autoscaling = true
release_channel                 = "REGULAR"
deletion_protection             = false
