output "registry_url" {
  description = "The URL to push/pull images"
  value       = module.artifact_registry.registry_url
}

output "repository_id" {
  description = "The ID of the Artifact Registry repository"
  value       = module.artifact_registry.repository_id
}
