resource "google_artifact_registry_repository" "sukko" {
  location      = var.region
  repository_id = var.repository_id
  description   = var.description
  format        = "DOCKER"
  project       = var.project_id

  cleanup_policies {
    id     = "keep-recent"
    action = "KEEP"

    most_recent_versions {
      keep_count = 10
    }
  }

  cleanup_policies {
    id     = "delete-old-untagged"
    action = "DELETE"

    condition {
      tag_state  = "UNTAGGED"
      older_than = "2592000s" # 30 days
    }
  }
}

# IAM for GKE nodes to pull images
resource "google_artifact_registry_repository_iam_member" "gke_reader" {
  for_each = toset(var.gke_service_accounts)

  project    = var.project_id
  location   = var.region
  repository = google_artifact_registry_repository.sukko.name
  role       = "roles/artifactregistry.reader"
  member     = "serviceAccount:${each.value}"
}

# IAM for CI/CD to push images
resource "google_artifact_registry_repository_iam_member" "cicd_writer" {
  for_each = toset(var.cicd_service_accounts)

  project    = var.project_id
  location   = var.region
  repository = google_artifact_registry_repository.sukko.name
  role       = "roles/artifactregistry.writer"
  member     = "serviceAccount:${each.value}"
}
