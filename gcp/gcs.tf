# https://github.com/GoogleCloudPlatform/cloud-foundation-fabric/blob/v52.0.0/modules/gcs/README.md

# GCS bucket for storing Terraform state
module "gcs-scrumpoker-iac" {
  source        = "git::https://github.com/GoogleCloudPlatform/cloud-foundation-fabric//modules/gcs?ref=v52.0.0"
  project_id    = module.project.project_id
  prefix        = module.project.project_id
  name          = "sp-iac-${local.region_shortnames[var.region]}"
  location      = var.region
  versioning    = true
  force_destroy = true
  lifecycle_rules = {
    lr-0 = {
      action = {
        type = "Delete"
      }
      condition = {
        num_newer_versions = 7
      }
    }
  }
}

# GCS bucket for Cloud Build source staging
module "gcs-scrumpoker-cloud-build" {
  source        = "git::https://github.com/GoogleCloudPlatform/cloud-foundation-fabric//modules/gcs?ref=v52.0.0"
  project_id    = module.project.project_id
  prefix        = module.project.project_id
  name          = "sp-build-${local.region_shortnames[var.region]}"
  location      = var.region
  versioning    = false
  force_destroy = true
  lifecycle_rules = {
    lr-0 = {
      action = {
        type = "Delete"
      }
      condition = {
        age        = 2
        with_state = "ANY"
      }
    }
  }
  iam = {
    "roles/storage.objectAdmin" = [
      module.service-account-cloud-build.iam_email
    ]
  }
  depends_on = [
    time_sleep.wait_for_service_account_cloud_build
  ]
}

# GCS bucket for storing the SQLite database
module "gcs-scrumpoker-database" {
  source        = "git::https://github.com/GoogleCloudPlatform/cloud-foundation-fabric//modules/gcs?ref=v52.0.0"
  project_id    = module.project.project_id
  prefix        = module.project.project_id
  name          = "sp-db-${local.region_shortnames[var.region]}"
  location      = var.region
  versioning    = false
  force_destroy = true
  # Delete stored rooms older 1 day
  lifecycle_rules = {
    lr-0 = {
      action = {
        type = "Delete"
      }
      condition = {
        age        = 1
        with_state = "ANY"
      }
    }
  }
  iam = {
    "roles/storage.objectAdmin" = [
      module.service-account-cloud-run-scrumpoker.iam_email
    ]
  }
  depends_on = [
    time_sleep.wait_for_service_account_cloud_run
  ]
}
