# =============================================================================
# GKE Standard - Develop Environment
# =============================================================================

# Project Configuration
project_id = "trim-array-480700-j7"

# Region & Zone
region = "us-central1"
zone   = "us-central1-a"

# Cluster Configuration
cluster_name = "odin-ws-develop"
environment  = "develop"
namespace    = "odin-std-develop"
network_name = "odin-ws-develop-vpc"

# Node Pool Configuration
node_machine_type = "e2-standard-4"
node_disk_size_gb = 50

# Spot VMs - 60-90% cheaper
use_spot_vms     = true
taint_spot_nodes = false

# Scaling - Fixed 2 nodes for develop
node_count         = 2
enable_autoscaling = false

# Features
enable_network_policy           = true
enable_vertical_pod_autoscaling = true
release_channel                 = "REGULAR"
deletion_protection             = false
