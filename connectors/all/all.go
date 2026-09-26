// Package all enregistre tous les connecteurs disponibles (import pour effet de bord).
package all

import (
	_ "github.com/kairn-io/kairn/connectors/agent"
	_ "github.com/kairn-io/kairn/connectors/demo"
	_ "github.com/kairn-io/kairn/connectors/focus"
	_ "github.com/kairn-io/kairn/connectors/kubernetes"
	_ "github.com/kairn-io/kairn/connectors/openstack"
	_ "github.com/kairn-io/kairn/connectors/outscale"
	_ "github.com/kairn-io/kairn/connectors/ovh"
	_ "github.com/kairn-io/kairn/connectors/prometheus"
	_ "github.com/kairn-io/kairn/connectors/scaleway"
	_ "github.com/kairn-io/kairn/connectors/webhooks"
)
