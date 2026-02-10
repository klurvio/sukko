# =============================================================================
# Foundation - Dev Environment
# =============================================================================

# Project Configuration
project_id = "odin-9e902"
region     = "us-central1"
vpc_name   = "odin-dev-vpc"

# WS cluster subnet (non-overlapping CIDRs)
ws_subnet_cidr   = "10.0.0.0/20"
ws_pods_cidr     = "10.1.0.0/16"
ws_services_cidr = "10.2.0.0/20"
