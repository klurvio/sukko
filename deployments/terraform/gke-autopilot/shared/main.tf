module "artifact_registry" {
  source     = "../../modules/artifact-registry"
  project_id = var.gcp_project
  region     = var.gcp_region

  repository_id = "odin"
  description   = "Container images for Odin WebSocket infrastructure"

  gke_service_accounts  = var.gke_service_accounts
  cicd_service_accounts = var.cicd_service_accounts
}
