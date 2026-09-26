# Provider Terraform / OpenTofu Kairn

Configuration « as code » d'une organisation Kairn (M-12) : paramètres, connecteurs, hiérarchie et règles d'allocation, budgets. Le provider n'utilise que l'API publique `/api/v1` avec un **jeton d'API** ; il a donc exactement les droits de ce jeton (RBAC, isolation de l'organisation).

## Configuration

```hcl
provider "kairn" {
  url   = "https://kairn.example.com" # ou KAIRN_URL
  token = var.kairn_token             # ou KAIRN_TOKEN (jeton kairn_…, rôle Admin recommandé)
  # organization_id : par défaut l'organisation du jeton (ou KAIRN_ORG)
}
```

Un jeton d'API est lié à une organisation : pour gérer plusieurs organisations (MSP), déclarer un alias de provider par organisation.

## Ressources

| Ressource | Rôle | Import |
|---|---|---|
| `kairn_organization` | Paramètres de l'organisation du jeton (nom, devise, langue, fuseau, TVA). La détruire **ne supprime pas** l'organisation (réservé à un propriétaire, depuis l'interface). | `terraform import kairn_organization.this <org_id>` |
| `kairn_connector` | Connecteur (lecture seule chez le fournisseur). `secrets` est en écriture seule : Kairn ne renvoie jamais les valeurs ; un secret supprimé hors Terraform est détecté et réappliqué. | `<connector_id>` (les secrets doivent ensuite être fournis) |
| `kairn_allocation_node` | Nœud de la hiérarchie (business unit, équipe, service, environnement). | `<node_id>` |
| `kairn_allocation_rule` | Règle d'allocation (conditions `eq`, `neq`, `in`, `regex`, `exists`, `prefix` sur `provider`, `type`, `name`, `region`, `label.<clé>`, `attr.<clé>`…). | `<rule_id>` |
| `kairn_budget` | Budget mensuel, trimestriel ou annuel, seuils d'alerte et alerte sur prévision. Montant en **chaîne décimale** (jamais de flottant). | `<budget_id>` |

Exemple complet : [`examples/main.tf`](examples/main.tf). Les paramètres et permissions de chaque type de connecteur sont décrits dans [`docs/connectors`](../docs/connectors/README.md).

## Développement

```bash
cd terraform-provider
go test ./... -cover        # tests contre une API Kairn simulée
go build -o terraform-provider-kairn
```

Pour tester localement avec Terraform ou OpenTofu, déclarer le binaire dans `~/.terraformrc` :

```hcl
provider_installation {
  dev_overrides { "kairn-io/kairn" = "/chemin/vers/terraform-provider" }
  direct {}
}
```

Le provider est un module Go distinct (`github.com/kairn-io/kairn/terraform-provider`) pour ne pas imposer les dépendances HashiCorp au reste du dépôt.
