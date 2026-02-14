# Google Cloud Set-Up

This directory contains the Terraform Infrastructure as Code (IaC) configuration and instructions to automatically create the Google Cloud Platform (GCP) infrastructure for the Scrum Poker app.

## Architecture

```mermaid
graph TD
    User(["👤 User"]) -->|HTTPS| CloudRun["🚀 Cloud Run (Scrum Poker App)"]
    
    CloudRun -->|Mount| GCS_DB["🗄️ GCS Bucket"]

    CloudBuild["🛠️ Cloud Build"] -->|Push| ArtReg["📦 Artifact Registry"]
    ArtReg -->|Deploy| CloudRun

    subgraph "Google Cloud Project"
        CloudRun
        GCS_DB
        ArtReg
        CloudBuild
        GCS_IAC["📜 GCS Bucket (TF State)"]
    end
```

## Project

A separate Google Cloud project is recommended for the Scrum Poker app.

Create a Google Cloud project with a attached billing account.

### IAM Roles

The **Owner role** (`roles/owner`) is the easiest option for deploying this project. If the Owner role is not possible (e.g., in enterprise environments with restricted permissions), the following specific roles must be assigned to your Google account on project level:

| Role ID | Role Name | Purpose |
|---------|-----------|---------|
| `roles/artifactregistry.admin` | Artifact Registry Administrator | Manage container images in Artifact Registry |
| `roles/cloudbuild.builds.editor` | Cloud Build Editor | Trigger and manage Cloud Build jobs |
| `roles/iam.roleViewer` | Role Viewer | Provides read access to all custom roles in the project. |
| `roles/iam.serviceAccountAdmin` | Service Account Admin | Create and manage service accounts. |
| `roles/iam.serviceAccountUser` | Service Account User | Run operations as the service account. |
| `roles/logging.admin` | Logging Admin | Access to all logging permissions, and dependent permissions. |
| `roles/monitoring.admin` | Monitoring Admin | All monitoring permissions. |
| `roles/orgpolicy.policyViewer` | Organization Policy Viewer | View organization policies |
| `roles/resourcemanager.projectIamAdmin` | Project IAM Admin | Access and administer a project IAM policies. |
| `roles/run.admin` | Cloud Run Admin | Deploy and manage Cloud Run services |
| `roles/serviceusage.serviceUsageAdmin` | Service Usage Admin | Enable and disable Google Cloud APIs |
| `roles/storage.admin` | Storage Admin | Manage Cloud Storage buckets |

> **Tip:** Use the [`assign-iam-roles.sh`](../tools/assign-iam-roles.sh) script in the `/tools` folder to automatically assign these roles to a user:
> ```bash
> ./tools/assign-iam-roles.sh PROJECT_ID user@example.com
> ```

### 1. Login

Authenticate with Google Cloud and set the quota project:

```bash
gcloud auth login --no-launch-browser
gcloud auth application-default login --no-launch-browser
```

Set the quota project and project where the resources will be created:

```bash
gcloud projects list
export GOOGLE_CLOUD_PROJECT="your-project-id"
gcloud config set project "$GOOGLE_CLOUD_PROJECT"
gcloud auth application-default set-quota-project "$GOOGLE_CLOUD_PROJECT"
```

### 2. Organization Policy

Make sure that the organization policy "Allowed ingress settings (Cloud Run)" `run.allowedIngress` is configured and set to `all`.

```yaml
run.allowedIngress
spec:
  rules:
  - values:
      allowedValues:
      - all
```

> That is the default setting.
> However, in some Google Cloud organizations, mainly those of large companies, these values may have been changed.

```bash
gcloud services enable "orgpolicy.googleapis.com" --project="$GOOGLE_CLOUD_PROJECT"
gcloud org-policies describe "run.allowedIngress" --effective --project="$GOOGLE_CLOUD_PROJECT"
```

If you are using the [Fabric FAST Project Factory](https://github.com/GoogleCloudPlatform/cloud-foundation-fabric), add the following to your `your-project-id.yaml`:

```yaml
org_policies:
  run.allowedIngress:
    rules:
      - allow:
          values:
            - all
```

See <https://cloud.google.com/resource-manager/docs/organization-policy/org-policy-constraints> for more information.

## Setup via Terraform

The majority of the required services and resources are configured via [Terraform](https://developer.hashicorp.com/terraform) Infrastructure as Code (IaC).

### 1. Configure

Navigate to the `gcp` directory (the directory of this README) and create the variables file:

```bash
cd gcp
```

Create a `terraform.tfvars` file with your configuration.

Google Cloud project ID:

```bash
printf 'project_id = "%s"\n' "$GOOGLE_CLOUD_PROJECT" > terraform.tfvars
```

(Optional) Google Cloud region:

```bash
printf 'region = "%s"\n' "us-central1" >> terraform.tfvars
```

For all available variables, see [variables.tf](variables.tf).

### 2. Apply

Initialize Terraform:

```bash
terraform init
```

Apply the configuration:

```bash
terraform apply
```

* Review the plan when prompted.
* Type `yes` and press Enter to confirm.

### (Optional) Migrate Terraform State to GCS

By default, Terraform stores state locally. For production environments or team usage, it's highly recommended to migrate this state to a remote **Google Cloud Storage (GCS)** backend.

Steps:
1.  Apply Terraform locally first.
2.  Copy `providers.tf.gcs` to `providers.tf` (configured for GCS backend).
3.  Run `terraform init -migrate-state` to copy your local state to the bucket.

## Rebuild Container Image

To rebuild the container image, run:

```bash
./build-container.sh
```

Apply the new container image to the Cloud Run service:

```bash
terraform apply
```

The used [Dockerfile](../Dockerfile) is located in the upper directory.

## Cleanup / Destroy

To destroy all resources created by Terraform, run:

```bash
terraform destroy
```

<!-- BEGIN_TF_DOCS -->
## Providers

| Name | Version |
|------|---------|
| <a name="provider_google"></a> [google](#provider\_google) | 7.19.0 |
| <a name="provider_local"></a> [local](#provider\_local) | 2.6.2 |
| <a name="provider_null"></a> [null](#provider\_null) | 3.2.4 |
| <a name="provider_time"></a> [time](#provider\_time) | 0.13.1 |

## Inputs

| Name | Description | Type | Default | Required |
|------|-------------|------|---------|:--------:|
| <a name="input_apis"></a> [apis](#input\_apis) | List of Google Cloud APIs to be enable | `list(string)` | <pre>[<br/>  "artifactregistry.googleapis.com",<br/>  "cloudbuild.googleapis.com",<br/>  "cloudresourcemanager.googleapis.com",<br/>  "compute.googleapis.com",<br/>  "iam.googleapis.com",<br/>  "logging.googleapis.com",<br/>  "orgpolicy.googleapis.com",<br/>  "run.googleapis.com",<br/>  "storage.googleapis.com"<br/>]</pre> | no |
| <a name="input_project_id"></a> [project\_id](#input\_project\_id) | Existing Google Cloud project ID | `string` | n/a | yes |
| <a name="input_region"></a> [region](#input\_region) | Google Cloud region name | `string` | `"us-central1"` | no |

## Outputs

| Name | Description |
|------|-------------|
| <a name="output_scrumpoker_url"></a> [scrumpoker\_url](#output\_scrumpoker\_url) | Service URL of the Scrum Poker app (Cloud Run) |
<!-- END_TF_DOCS -->
