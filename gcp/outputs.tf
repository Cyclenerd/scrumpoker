# Service URL of the Scrum Poker app (Cloud Run)
output "scrumpoker_url" {
  value = module.cloud_run_github_runners_manager.service_uri
}

# Generate Cloud Build configuration for building the container image
resource "local_file" "cloudbuild-scrumpoker-config" {
  filename        = "${path.module}/cloudbuild-container.yaml"
  file_permission = "0640"
  content = templatefile("${path.module}/cloudbuild-container.template.yaml", {
    repository_url        = module.artifact-registry-container.url,
    build_service_account = module.service-account-cloud-build.id # Not email
  })
}

# Generate shell script to trigger Cloud Build for the container image
resource "local_file" "cloudbuild-github-runners-manager-script" {
  filename        = "${path.module}/build-container.sh"
  file_permission = "0750"
  content = templatefile("${path.module}/build-container.template.sh", {
    region     = var.region
    project_id = module.project.project_id
    bucket     = module.gcs-scrumpoker-cloud-build.name
  })
}

# Trigger the build of the container image when relevant files change
resource "null_resource" "build-github-runners-manager-container" {
  triggers = {
    script_hash     = sha256(local_file.cloudbuild-github-runners-manager-script.content)
    config_hash     = sha256(local_file.cloudbuild-scrumpoker-config.content)
    dockerfile_hash = sha256(file("${path.module}/../Dockerfile"))
  }

  provisioner "local-exec" {
    command = local_file.cloudbuild-github-runners-manager-script.filename
  }

  depends_on = [
    module.project,
    module.service-account-cloud-build,
    time_sleep.wait_for_service_account_cloud_build,
    module.artifact-registry-container,
    module.gcs-scrumpoker-cloud-build,
  ]
}

# Generate providers.tf for GCS backend (helper for migration/setup)
resource "local_file" "terraform-providers-file-gcs" {
  filename        = "${path.module}/providers.tf.gcs"
  file_permission = "0640"
  content = templatefile("${path.module}/providers.tf.template", {
    bucket = module.gcs-scrumpoker-iac.name
  })
}
