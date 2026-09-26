variable "project_id" {
  description = "Identifiant du projet OVHcloud Public Cloud"
  type        = string
}

variable "environment" {
  description = "Environnement (staging, production)"
  type        = string
  validation {
    condition     = contains(["staging", "production"], var.environment)
    error_message = "environment must be staging or production"
  }
}

variable "region" {
  description = "Région OVHcloud (UE uniquement)"
  type        = string
  default     = "GRA11"
  validation {
    condition     = can(regex("^(GRA|SBG|RBX|DE|WAW|UK|EU-WEST-PAR)", var.region))
    error_message = "Kairn SaaS must be hosted in an EU region"
  }
}

variable "storage_region" {
  description = "Région du stockage objet"
  type        = string
  default     = "GRA"
}

variable "vlan_id" {
  type    = number
  default = 100
}

variable "subnet_cidr" {
  type    = string
  default = "10.42.0.0/16"
}

variable "kubernetes_version" {
  type    = string
  default = "1.31"
}

variable "system_flavor" {
  type    = string
  default = "b3-16"
}

variable "system_nodes" {
  type    = number
  default = 3
}

variable "system_nodes_max" {
  type    = number
  default = 8
}

variable "data_flavor" {
  type    = string
  default = "i1-45" # NVMe local pour ClickHouse
}

variable "data_nodes" {
  type    = number
  default = 3
}

variable "postgres_plan" {
  type    = string
  default = "business"
}

variable "postgres_flavor" {
  type    = string
  default = "db1-15"
}

variable "postgres_nodes" {
  type    = number
  default = 2
}
