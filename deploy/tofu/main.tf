# Infrastructure SaaS de Kairn sur OVHcloud (hébergement UE, souveraineté).
#   tofu init && tofu plan -var-file=environments/production.tfvars
# Réseau privé (vRack), Kubernetes managé (MKS), PostgreSQL managé, stockage objet S3.
# ClickHouse, NATS, Vault et cert-manager sont déployés dans le cluster (voir README).

terraform {
  required_version = ">= 1.8"
  required_providers {
    ovh = {
      source  = "ovh/ovh"
      version = "~> 2.0"
    }
  }
  backend "s3" {
    # Configuration fournie à l'initialisation : tofu init -backend-config=environments/<env>.backend.hcl
  }
}

provider "ovh" {
  endpoint = "ovh-eu"
}

locals {
  name = "kairn-${var.environment}"
  tags = { project = "kairn", environment = var.environment, managed-by = "opentofu" }
}

# ---------------------------------------------------------------- réseau privé
resource "ovh_cloud_project_network_private" "net" {
  service_name = var.project_id
  name         = local.name
  regions      = [var.region]
  vlan_id      = var.vlan_id
}

resource "ovh_cloud_project_network_private_subnet" "subnet" {
  service_name = var.project_id
  network_id   = ovh_cloud_project_network_private.net.id
  region       = var.region
  network      = var.subnet_cidr
  start        = cidrhost(var.subnet_cidr, 10)
  end          = cidrhost(var.subnet_cidr, 250)
  dhcp         = true
  no_gateway   = false
}

# ---------------------------------------------------------------- Kubernetes managé
resource "ovh_cloud_project_kube" "k8s" {
  service_name       = var.project_id
  name               = local.name
  region             = var.region
  version            = var.kubernetes_version
  private_network_id = tolist(ovh_cloud_project_network_private.net.regions_attributes[*].openstackid)[0]
  update_policy      = "MINIMAL_DOWNTIME"

  private_network_configuration {
    default_vrack_gateway              = ""
    private_network_routing_as_default = false
  }

  depends_on = [ovh_cloud_project_network_private_subnet.subnet]
}

resource "ovh_cloud_project_kube_nodepool" "system" {
  service_name  = var.project_id
  kube_id       = ovh_cloud_project_kube.k8s.id
  name          = "system"
  flavor_name   = var.system_flavor
  desired_nodes = var.system_nodes
  min_nodes     = var.system_nodes
  max_nodes     = var.system_nodes_max
  autoscale     = true
  anti_affinity = true
}

resource "ovh_cloud_project_kube_nodepool" "data" {
  service_name  = var.project_id
  kube_id       = ovh_cloud_project_kube.k8s.id
  name          = "data" # ClickHouse et NATS (disques locaux rapides)
  flavor_name   = var.data_flavor
  desired_nodes = var.data_nodes
  min_nodes     = var.data_nodes
  max_nodes     = var.data_nodes
  autoscale     = false
  anti_affinity = true

  template {
    metadata {
      labels      = { "kairn.io/pool" = "data" }
      annotations = {}
      finalizers  = []
    }
    spec {
      unschedulable = false
      taints = [{
        effect = "NoSchedule"
        key    = "kairn.io/pool"
        value  = "data"
      }]
    }
  }
}

# ---------------------------------------------------------------- PostgreSQL managé (config, inventaire, RBAC)
resource "ovh_cloud_project_database" "pg" {
  service_name = var.project_id
  description  = local.name
  engine       = "postgresql"
  version      = "16"
  plan         = var.postgres_plan
  flavor       = var.postgres_flavor

  dynamic "nodes" {
    for_each = range(var.postgres_nodes)
    content {
      region     = var.region
      network_id = tolist(ovh_cloud_project_network_private.net.regions_attributes[*].openstackid)[0]
      subnet_id  = ovh_cloud_project_network_private_subnet.subnet.id
    }
  }

  ip_restrictions {
    description = "réseau privé de la plateforme"
    ip          = var.subnet_cidr
  }
}

resource "ovh_cloud_project_database_database" "kairn" {
  service_name = var.project_id
  engine       = "postgresql"
  cluster_id   = ovh_cloud_project_database.pg.id
  name         = "kairn"
}

# Les rôles kairn_owner (BYPASSRLS, migrations) et kairn_app (NOBYPASSRLS) sont créés
# avec deploy/postgres/init-roles.sql par l'administrateur (voir ADR-0002).
resource "ovh_cloud_project_database_postgresql_user" "admin" {
  service_name = var.project_id
  cluster_id   = ovh_cloud_project_database.pg.id
  name         = "kairn_admin"
  roles        = ["replication"]
}

# ---------------------------------------------------------------- stockage objet (rapports, exports)
resource "ovh_cloud_project_user" "s3" {
  service_name = var.project_id
  description  = "${local.name} object storage"
  role_names   = ["objectstore_operator"]
}

resource "ovh_cloud_project_user_s3_credential" "s3" {
  service_name = var.project_id
  user_id      = ovh_cloud_project_user.s3.id
}

resource "ovh_cloud_project_storage" "reports" {
  service_name = var.project_id
  region_name  = var.storage_region
  name         = local.name

  encryption = {
    sse_algorithm = "AES256"
  }
  versioning = {
    status = "enabled"
  }
}
