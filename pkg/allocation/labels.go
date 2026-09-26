package allocation

import "github.com/kairn-io/kairn/pkg/model"

// Relations par lesquelles les labels se propagent du parent vers l'enfant :
// projet → VM, namespace → workload → pod, VM → volume attaché.
var inheritRelations = map[string]bool{
	model.RelContains:   true,
	model.RelOwns:       true,
	model.RelAttachedTo: true,
}

const maxInheritDepth = 6

// syntheticLabels sont dérivés des attributs Kubernetes quand ils existent.
var syntheticLabels = []string{"k8s.namespace", "k8s.cluster", "k8s.workload"}

// Labeler calcule les labels effectifs (propres + hérités des parents).
// Les labels propres sont prioritaires sur les labels hérités.
type Labeler struct {
	latest  func(id string) (model.Resource, bool)
	parents map[string][]model.ResourceEdge
	cache   map[string]map[string]string
}

// NewLabeler construit un calculateur à partir des ressources (dernière
// version par identifiant) et des arêtes courantes.
func NewLabeler(latest func(id string) (model.Resource, bool), edges []model.ResourceEdge) *Labeler {
	l := &Labeler{latest: latest, parents: map[string][]model.ResourceEdge{}, cache: map[string]map[string]string{}}
	for _, e := range edges {
		l.parents[e.ChildID] = append(l.parents[e.ChildID], e)
	}
	return l
}

// NewLabelerFromList construit un calculateur à partir d'une liste de ressources.
func NewLabelerFromList(resources []model.Resource, edges []model.ResourceEdge) *Labeler {
	byID := make(map[string]model.Resource, len(resources))
	for _, r := range resources {
		if cur, ok := byID[r.ID]; !ok || r.ValidFrom.After(cur.ValidFrom) {
			byID[r.ID] = r
		}
	}
	return NewLabeler(func(id string) (model.Resource, bool) { r, ok := byID[id]; return r, ok }, edges)
}

// Labels renvoie les labels effectifs d'une ressource.
func (l *Labeler) Labels(id string) map[string]string {
	if out, ok := l.cache[id]; ok {
		return out
	}
	out := map[string]string{}
	l.cache[id] = out
	l.collect(id, out, 0, map[string]bool{})
	return out
}

func (l *Labeler) collect(id string, out map[string]string, depth int, seen map[string]bool) {
	if seen[id] || depth > maxInheritDepth {
		return
	}
	seen[id] = true
	if r, ok := l.latest(id); ok {
		for k, v := range r.Labels {
			if _, set := out[k]; !set {
				out[k] = v
			}
		}
		for _, attr := range syntheticLabels {
			if v := r.Attr(attr); v != "" {
				if _, set := out[attr]; !set {
					out[attr] = v
				}
			}
		}
	}
	for _, ed := range l.parents[id] {
		if inheritRelations[ed.Relation] {
			l.collect(ed.ParentID, out, depth+1, seen)
		}
	}
}

// Subject construit le sujet d'évaluation des règles pour une ressource.
func (l *Labeler) Subject(id string) Subject {
	r, _ := l.latest(id)
	return Subject{Resource: r, Labels: l.Labels(id)}
}
