# https://github.com/GoogleCloudPlatform/cloud-foundation-fabric/blob/v52.0.0/modules/iam-service-account/README.md

# Service Account for the Runners Manager (Cloud Run)
module "service-account-cloud-run-scrumpoker" {
  source       = "git::https://github.com/GoogleCloudPlatform/cloud-foundation-fabric//modules/iam-service-account?ref=v53.0.0"
  project_id   = module.project.project_id
  name         = "scrumpoker"
  display_name = "Cloud Run - Scrum Poker (Terraform managed)"
  iam_project_roles = {
    (module.project.project_id) = [
      "roles/logging.logWriter",
      "roles/monitoring.metricWriter",
    ]
  }
}

# Wait for service account to be fully propagated in Google Cloud IAM
resource "time_sleep" "wait_for_service_account_cloud_run" {
  depends_on = [
    module.service-account-cloud-run-scrumpoker
  ]
  create_duration = "30s"
}

# Service Account for Cloud Build (Container image creation)
module "service-account-cloud-build" {
  source       = "git::https://github.com/GoogleCloudPlatform/cloud-foundation-fabric//modules/iam-service-account?ref=v52.0.0"
  project_id   = module.project.project_id
  name         = "cloud-build-container"
  display_name = "Cloud Build - Create container (Terraform managed)"
  iam_project_roles = {
    (module.project.project_id) = [
      "roles/logging.logWriter",
    ]
  }
}

# Wait for service account to be fully propagated in Google Cloud IAM
resource "time_sleep" "wait_for_service_account_cloud_build" {
  depends_on = [
    module.service-account-cloud-build
  ]
  create_duration = "30s"
}
