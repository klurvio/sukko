# =============================================================================
# Foundation - Shared VPC, Static IPs, Firewall Rules
# =============================================================================
# These resources persist across cluster destroy/recreate cycles.
# Static IPs survive cluster lifecycle changes.

module "foundation" {
  source = "../../../modules/foundation"

  project_id = var.project_id
  region     = var.region
  vpc_name   = var.vpc_name

  # WS cluster subnet
  ws_subnet_cidr   = var.ws_subnet_cidr
  ws_pods_cidr     = var.ws_pods_cidr
  ws_services_cidr = var.ws_services_cidr
}
