# Google Cloud APIs to enable for the project
variable "apis" {
  description = "List of Google Cloud APIs to be enable"
  type        = list(string)
  default = [
    "artifactregistry.googleapis.com",
    "cloudbuild.googleapis.com",
    "cloudresourcemanager.googleapis.com",
    "compute.googleapis.com",
    "iam.googleapis.com",
    "logging.googleapis.com",
    "orgpolicy.googleapis.com",
    "run.googleapis.com",
    "storage.googleapis.com",
  ]
}

# Google Cloud project ID where resources will be created
variable "project_id" {
  description = "Existing Google Cloud project ID"
  type        = string
  nullable    = false

  # https://cloud.google.com/resource-manager/docs/creating-managing-projects#before_you_begin
  validation {
    # Must be 6 to 30 characters in length.
    # Can only contain lowercase letters, numbers, and hyphens.
    # Must start with a letter.
    # Cannot end with a hyphen.
    condition     = can(regex("^[a-z][a-z0-9-]{4,28}[a-z0-9]$", var.project_id))
    error_message = "Invalid Google Cloud project ID!"
  }
}

# Google Cloud region for deploying resources
variable "region" {
  description = "Google Cloud region name"
  type        = string
  default     = "us-central1"
  nullable    = false

  validation {
    condition     = can(regex("^[a-z][-a-z]+[0-9]$", var.region))
    error_message = "Invalid Google Cloud region name!"
  }
}
