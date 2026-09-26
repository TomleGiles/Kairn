output "kubeconfig" {
  description = "Kubeconfig du cluster (sensible)"
  value       = ovh_cloud_project_kube.k8s.kubeconfig
  sensitive   = true
}

output "postgres_endpoint" {
  description = "Point d'accès PostgreSQL (réseau privé)"
  value       = ovh_cloud_project_database.pg.endpoints
}

output "s3_bucket" {
  value = ovh_cloud_project_storage.reports.name
}

output "s3_access_key" {
  value     = ovh_cloud_project_user_s3_credential.s3.access_key_id
  sensitive = true
}

output "s3_secret_key" {
  value     = ovh_cloud_project_user_s3_credential.s3.secret_access_key
  sensitive = true
}
