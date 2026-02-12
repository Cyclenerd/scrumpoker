# Artifact Registry for storing the Scrum Poker container image
# https://github.com/GoogleCloudPlatform/cloud-foundation-fabric/blob/v52.0.0/modules/artifact-registry/README.md
module "artifact-registry-container" {
  source                 = "git::https://github.com/GoogleCloudPlatform/cloud-foundation-fabric//modules/artifact-registry?ref=v53.0.0"
  project_id             = module.project.project_id
  location               = var.region
  name                   = "container-scrumpoker"
  description            = "Registry for Scrum Poker (Terraform-managed)"
  format                 = { docker = { standard = {} } }
  cleanup_policy_dry_run = false
  cleanup_policies = {
    keep-3-versions = {
      action = "KEEP"
      most_recent_versions = {
        keep_count = 3
      }
    }
    delete = {
      action = "DELETE"
      condition = {
        tag_state = "UNTAGGED"
      }
    }
  }
  iam = {
    "roles/artifactregistry.writer" = [
      module.service-account-cloud-build.iam_email
    ]
    "roles/artifactregistry.reader" = [
      module.service-account-cloud-run-scrumpoker.iam_email
    ]
  }
  depends_on = [
    time_sleep.wait_for_service_account_cloud_build
  ]
}
