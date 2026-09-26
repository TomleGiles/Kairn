// Package ids génère les identifiants. Les entités utilisent des UUIDv7
// (ordonnés dans le temps, utilisables comme curseur de pagination) ; les
// ressources d'inventaire utilisent des UUIDv5 déterministes afin que
// l'ingestion soit idempotente et rejouable.
package ids

import (
	"strings"

	"github.com/google/uuid"
)

// resourceNS est l'espace de noms UUIDv5 des ressources Kairn. Ne jamais le modifier :
// cela changerait l'identifiant de toutes les ressources existantes.
var resourceNS = uuid.MustParse("6f1d7c2e-8a4b-4f0e-9b1a-3c5d7e9f0a2b")

// New renvoie un nouvel UUIDv7.
func New() string {
	id, err := uuid.NewV7()
	if err != nil {
		// NewV7 n'échoue que si la source d'aléa est indisponible.
		return uuid.NewString()
	}
	return id.String()
}

// Resource renvoie l'identifiant stable d'une ressource d'inventaire.
func Resource(orgID, connectorID, resourceType, externalID string) string {
	key := strings.Join([]string{orgID, connectorID, resourceType, externalID}, "\x1f")
	return uuid.NewSHA1(resourceNS, []byte(key)).String()
}

// Valid indique si s est un UUID syntaxiquement valide.
func Valid(s string) bool {
	_, err := uuid.Parse(s)
	return err == nil
}
