package costengine

import (
	"github.com/kairn-io/kairn/pkg/allocation"
)

// effectiveLabels renvoie les labels propres de la ressource complétés par
// ceux de ses parents (voir allocation.Labeler).
func (e *engine) effectiveLabels(id string) map[string]string {
	return e.labeler.Labels(id)
}

// subject construit le sujet d'évaluation des règles pour une ressource.
func (e *engine) subject(id string) allocation.Subject {
	return e.labeler.Subject(id)
}
