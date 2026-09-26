// Package allocation évalue les règles d'allocation (M-04) et manipule
// l'arbre organisation → BU → équipe → service → environnement.
package allocation

import (
	"fmt"
	"regexp"
	"sort"
	"strings"

	"github.com/kairn-io/kairn/pkg/model"
)

// Subject est ce qui est évalué par une règle : une ressource et ses labels
// effectifs (propres + hérités des parents).
type Subject struct {
	Resource model.Resource
	Labels   map[string]string
}

// FieldValue renvoie la valeur d'un champ de condition et sa présence.
func FieldValue(s Subject, field string) (string, bool) {
	r := s.Resource
	switch field {
	case "provider":
		return r.Provider, r.Provider != ""
	case "type":
		return r.Type, r.Type != ""
	case "name":
		return r.Name, r.Name != ""
	case "region":
		return r.Region, r.Region != ""
	case "connector_id":
		return r.ConnectorID, r.ConnectorID != ""
	case "external_id":
		return r.ExternalID, r.ExternalID != ""
	case "resource_id":
		return r.ID, r.ID != ""
	}
	if k, ok := strings.CutPrefix(field, "label."); ok {
		v, present := s.Labels[k]
		return v, present
	}
	if k, ok := strings.CutPrefix(field, "attr."); ok {
		if _, present := r.Attributes[k]; !present {
			return "", false
		}
		return r.Attr(k), true
	}
	return "", false
}

type compiledCond struct {
	model.Condition
	re *regexp.Regexp
}

func compileCond(c model.Condition) (compiledCond, error) {
	cc := compiledCond{Condition: c}
	switch c.Op {
	case model.OpEq, model.OpNeq, model.OpIn, model.OpExists, model.OpPrefix:
	case model.OpRegex:
		re, err := regexp.Compile(c.Value)
		if err != nil {
			return cc, fmt.Errorf("allocation: invalid regex %q: %w", c.Value, err)
		}
		cc.re = re
	default:
		return cc, fmt.Errorf("allocation: unknown operator %q", c.Op)
	}
	if c.Field == "" {
		return cc, fmt.Errorf("allocation: empty field")
	}
	return cc, nil
}

func (c compiledCond) match(s Subject) bool {
	v, present := FieldValue(s, c.Field)
	switch c.Op {
	case model.OpExists:
		return present
	case model.OpEq:
		return present && v == c.Value
	case model.OpNeq:
		return !present || v != c.Value
	case model.OpIn:
		if !present {
			return false
		}
		for _, x := range c.Values {
			if x == v {
				return true
			}
		}
		return false
	case model.OpPrefix:
		return present && strings.HasPrefix(v, c.Value)
	case model.OpRegex:
		return present && c.re.MatchString(v)
	}
	return false
}

// Conditions est un ensemble de conditions compilées (toutes doivent correspondre).
type Conditions []compiledCond

// CompileConditions compile une liste de conditions.
func CompileConditions(conds []model.Condition) (Conditions, error) {
	out := make(Conditions, 0, len(conds))
	for _, c := range conds {
		cc, err := compileCond(c)
		if err != nil {
			return nil, err
		}
		out = append(out, cc)
	}
	return out, nil
}

// Match indique si toutes les conditions correspondent. Une liste vide ne correspond à rien.
func (cs Conditions) Match(s Subject) bool {
	if len(cs) == 0 {
		return false
	}
	for _, c := range cs {
		if !c.match(s) {
			return false
		}
	}
	return true
}

type compiledRule struct {
	rule  model.AllocationRule
	conds Conditions
}

// Matcher évalue les règles par priorité croissante ; la première qui correspond gagne.
type Matcher struct {
	rules []compiledRule
}

// Compile prépare les règles actives.
func Compile(rules []model.AllocationRule) (*Matcher, error) {
	m := &Matcher{}
	for _, r := range rules {
		if !r.Enabled {
			continue
		}
		conds, err := CompileConditions(r.Conditions)
		if err != nil {
			return nil, fmt.Errorf("rule %s: %w", r.ID, err)
		}
		m.rules = append(m.rules, compiledRule{rule: r, conds: conds})
	}
	sort.SliceStable(m.rules, func(i, j int) bool {
		if m.rules[i].rule.Priority != m.rules[j].rule.Priority {
			return m.rules[i].rule.Priority < m.rules[j].rule.Priority
		}
		return m.rules[i].rule.ID < m.rules[j].rule.ID
	})
	return m, nil
}

// Match renvoie le nœud attribué et la règle, ou UnallocatedNodeID.
func (m *Matcher) Match(s Subject) (nodeID, ruleID string) {
	for _, r := range m.rules {
		if r.conds.Match(s) {
			return r.rule.NodeID, r.rule.ID
		}
	}
	return model.UnallocatedNodeID, ""
}

// ---------------------------------------------------------------- arbre

// Tree indexe les nœuds d'allocation.
type Tree struct {
	byID     map[string]model.AllocationNode
	children map[string][]string
}

// NewTree construit l'index.
func NewTree(nodes []model.AllocationNode) *Tree {
	t := &Tree{byID: map[string]model.AllocationNode{}, children: map[string][]string{}}
	for _, n := range nodes {
		t.byID[n.ID] = n
		parent := ""
		if n.ParentID != nil {
			parent = *n.ParentID
		}
		t.children[parent] = append(t.children[parent], n.ID)
	}
	for k := range t.children {
		sort.Strings(t.children[k])
	}
	return t
}

// Node renvoie un nœud.
func (t *Tree) Node(id string) (model.AllocationNode, bool) {
	n, ok := t.byID[id]
	return n, ok
}

// Children renvoie les enfants directs (racines si id vide).
func (t *Tree) Children(id string) []string { return t.children[id] }

// Subtree renvoie id et tous ses descendants.
func (t *Tree) Subtree(id string) []string {
	out := []string{id}
	for i := 0; i < len(out); i++ {
		out = append(out, t.children[out[i]]...)
	}
	return out
}

// Ancestors renvoie les ancêtres de id, de la racine vers le parent direct.
func (t *Tree) Ancestors(id string) []string {
	var out []string
	seen := map[string]bool{}
	n, ok := t.byID[id]
	for ok && n.ParentID != nil && !seen[*n.ParentID] {
		seen[*n.ParentID] = true
		out = append([]string{*n.ParentID}, out...)
		n, ok = t.byID[*n.ParentID]
	}
	return out
}

// PathFor calcule le chemin matérialisé d'un nœud ("/racine/…/id/").
func (t *Tree) PathFor(id string) string {
	return "/" + strings.Join(append(t.Ancestors(id), id), "/") + "/"
}

// AllowedNodes calcule l'ensemble des nœuds visibles pour des scopes RBAC.
// Scopes vides = aucune restriction (nil).
func (t *Tree) AllowedNodes(scopes []string) []string {
	if len(scopes) == 0 {
		return nil
	}
	seen := map[string]bool{}
	var out []string
	for _, s := range scopes {
		for _, id := range t.Subtree(s) {
			if !seen[id] {
				seen[id] = true
				out = append(out, id)
			}
		}
	}
	sort.Strings(out)
	return out
}

// ValidateParent vérifie qu'un rattachement ne crée pas de cycle et respecte la hiérarchie.
func (t *Tree) ValidateParent(id, parentID, kind string) error {
	if parentID == "" {
		return nil
	}
	p, ok := t.byID[parentID]
	if !ok {
		return fmt.Errorf("allocation: unknown parent %s", parentID)
	}
	if id != "" {
		for _, a := range append(t.Ancestors(parentID), parentID) {
			if a == id {
				return fmt.Errorf("allocation: cycle detected")
			}
		}
	}
	if Level(kind) <= Level(p.Kind) {
		return fmt.Errorf("allocation: a %s cannot be placed under a %s", kind, p.Kind)
	}
	return nil
}

// Level renvoie la profondeur logique d'un type de nœud.
func Level(kind string) int {
	switch kind {
	case model.NodeOrganization:
		return 0
	case model.NodeBusinessUnit:
		return 1
	case model.NodeTeam:
		return 2
	case model.NodeService:
		return 3
	case model.NodeEnvironment:
		return 4
	}
	return 99
}
