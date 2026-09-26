// Commande terraform-provider-kairn : provider Terraform / OpenTofu de Kairn
// (M-12). Il gère, avec un jeton d'API scoppé, la configuration « as code »
// d'une organisation : paramètres, connecteurs, hiérarchie et règles
// d'allocation, budgets. Il n'utilise que l'API publique /api/v1.
package main

import (
	"context"
	"flag"
	"log"

	"github.com/hashicorp/terraform-plugin-framework/providerserver"

	"github.com/kairn-io/kairn/terraform-provider/internal/provider"
)

var version = "dev"

func main() {
	debug := flag.Bool("debug", false, "exécute le provider en mode débogage (delve)")
	flag.Parse()
	err := providerserver.Serve(context.Background(), provider.New(version), providerserver.ServeOpts{
		Address: "registry.terraform.io/kairn-io/kairn",
		Debug:   *debug,
	})
	if err != nil {
		log.Fatal(err)
	}
}
