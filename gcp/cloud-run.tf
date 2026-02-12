# Get the container image from Artifact Registry
# https://registry.terraform.io/providers/hashicorp/google/latest/docs/data-sources/artifact_registry_docker_image
data "google_artifact_registry_docker_image" "container-image-scrumpoker" {
  project       = module.project.project_id
  location      = var.region
  repository_id = module.artifact-registry-container.name
  image_name    = "app:latest" # Defined in cloudbuild-container.template.yaml
  depends_on = [
    null_resource.build-github-runners-manager-container
  ]
}

# Deploy the Scrum Poker app service on Cloud Run
# https://github.com/GoogleCloudPlatform/cloud-foundation-fabric/blob/v52.0.0/modules/cloud-run-v2/README.md
module "cloud_run_github_runners_manager" {
  source     = "git::https://github.com/GoogleCloudPlatform/cloud-foundation-fabric//modules/cloud-run-v2?ref=v52.0.0"
  project_id = module.project.project_id
  name       = "scrumpoker-${local.region_shortnames[var.region]}"
  type       = "SERVICE"
  region     = var.region
  service_config = {
    # Second generation Cloud Run for faster CPU and bucket mount
    gen2_execution_environment = true
    scaling = {
      min_instance_count = 0
      max_instance_count = 1
    }
    timeout = "300s" # 5min
  }
  containers = {
    scrumpoker = {
      image = data.google_artifact_registry_docker_image.container-image-scrumpoker.self_link
      resources = {
        limits = {
          cpu    = "1000m"
          memory = "512Mi"
        }
        cpu_idle          = true # Charged only when processing requests. CPU is limited outside of requests.
        startup_cpu_boost = true # Start containers faster by allocating more CPU during startup time.
      }
      env = {
        SCRUMPOKER_STORAGE_DIR = "/var/lib/database"
      }
      volume_mounts = {
        "database" = "/var/lib/database"
      }
    }
  }
  volumes = {
    database = {
      gcs = {
        bucket       = module.gcs-scrumpoker-database.name
        is_read_only = false
      }
    }
  }
  service_account_config = {
    create = false
    email  = module.service-account-cloud-run-scrumpoker.email
  }
  iam = {
    "roles/run.invoker" = ["allUsers"] # Public
  }
  deletion_protection = false
  depends_on = [
    time_sleep.wait_for_service_account_cloud_run
  ]
}
