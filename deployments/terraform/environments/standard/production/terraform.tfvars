# =============================================================================
# GKE Standard - Production Environment
# =============================================================================

# Project Configuration
project_id = "trim-array-480700-j7"

# Region & Zone
region = "us-central1"
zone   = "us-central1-a"

# Cluster Configuration
cluster_name = "odin-ws-production"
environment  = "production"
namespace    = "odin-std-production"
network_name = "odin-ws-production-vpc"

# Node Pool Configuration
node_machine_type = "e2-standard-4"
node_disk_size_gb = 50

# Spot VMs - enabled for cost savings (consider disabling for strict uptime)
use_spot_vms     = true
taint_spot_nodes = false

# Scaling - Autoscaling 1-5 nodes for production
node_count         = 2
enable_autoscaling = true
min_node_count     = 1
max_node_count     = 5

# Features
enable_network_policy           = true
enable_vertical_pod_autoscaling = true
release_channel                 = "STABLE"  # More stable for production
deletion_protection             = true      # Protect against accidental deletion
