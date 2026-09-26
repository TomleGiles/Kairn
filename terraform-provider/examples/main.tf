terraform {
  required_providers {
    kairn = {
      source  = "kairn-io/kairn"
      version = "~> 0.1"
    }
  }
}

# URL et jeton via KAIRN_URL / KAIRN_TOKEN (jeton d'API, rôle Admin) : ne jamais les versionner.
provider "kairn" {}

variable "openstack_app_credential_secret" {
  type      = string
  sensitive = true
}

resource "kairn_organization" "this" {
  currency = "EUR"
  locale   = "fr"
  timezone = "Europe/Paris"
  vat_rate = "20"
}

resource "kairn_connector" "ovh_gra" {
  type = "openstack"
  name = "OVHcloud GRA11"
  settings = {
    auth_url                  = "https://auth.cloud.ovh.net/v3"
    region                    = "GRA11"
    application_credential_id = "a1b2c3"
    pricing_provider          = "ovh"
  }
  secrets = {
    application_credential_secret = var.openstack_app_credential_secret
  }
  backfill_days = 90
}

resource "kairn_allocation_node" "produit" {
  kind = "business_unit"
  name = "Produit"
}

resource "kairn_allocation_node" "data" {
  kind      = "team"
  name      = "Data"
  parent_id = kairn_allocation_node.produit.id
}

resource "kairn_allocation_rule" "data" {
  node_id  = kairn_allocation_node.data.id
  name     = "Ressources de l'équipe Data"
  priority = 10
  conditions = [
    { field = "label.team", op = "in", values = ["data", "analytics"] },
  ]
}

resource "kairn_budget" "data" {
  name       = "Équipe Data — mensuel"
  period     = "monthly"
  amount     = "8000.00"
  node_id    = kairn_allocation_node.data.id
  thresholds = [50, 80, 100]
}
