// Package inventory applique une synchronisation d'inventaire à l'historique
// (M-02) : création de versions, fermeture des ressources disparues,
// historisation des arêtes du graphe et production d'événements de changement.
// L'opération est idempotente : réappliquer la même observation ne change rien.
package inventory

import (
	"context"
	"fmt"
	"reflect"
	"sort"
	"time"

	"github.com/kairn-io/kairn/pkg/connector"
	"github.com/kairn-io/kairn/pkg/ids"
	"github.com/kairn-io/kairn/pkg/model"
	"github.com/kairn-io/kairn/pkg/store"
	"github.com/kairn-io/kairn/pkg/tenancy"
)

// Attributs dont la modification crée une nouvelle version (ils influent sur
// le coût ou l'allocation). Les autres attributs (ex. horodatage d'activité)
// sont mis à jour sans nouvelle version… en pratique ils sont ignorés.
var versionedAttributes = []string{
	"flavor", "instance_type", "vcpus", "ram_gb", "size_gb", "volume_type", "status", "billing_state",
	"license", "storage_class", "ip_kind", "engine", "cpu_request_cores", "mem_request_gb",
	"cpu_capacity_cores", "mem_capacity_gb", "attached_to", "k8s.namespace", "k8s.cluster", "k8s.workload",
	"replicas", "control_plane_tier", "project_id", "env",
}

// Result résume l'application d'une synchronisation.
type Result struct {
	Created  int
	Updated  int
	Deleted  int
	Observed int
	Events   []model.Event
}

// Snapshot est une observation d'inventaire d'un connecteur.
type Snapshot struct {
	ConnectorID string
	Provider    string
	ObservedAt  time.Time
	Resources   []connector.Resource
	// Complete : vrai si la synchronisation s'est terminée sans erreur. Seule
	// une observation complète permet de marquer des ressources supprimées.
	Complete bool
}

// Apply applique un instantané d'inventaire dans une transaction.
func Apply(ctx context.Context, st store.Store, snap Snapshot) (Result, error) {
	org, err := tenancy.OrgID(ctx)
	if err != nil {
		return Result{}, err
	}
	var res Result
	err = st.InTx(ctx, func(ctx context.Context) error {
		var err error
		res, err = apply(ctx, st, org, snap)
		return err
	})
	return res, err
}

func apply(ctx context.Context, st store.Store, org string, snap Snapshot) (Result, error) {
	at := snap.ObservedAt.UTC().Truncate(time.Second)
	current, err := st.Resources().Current(ctx, store.ResourceFilter{ConnectorID: snap.ConnectorID})
	if err != nil {
		return Result{}, fmt.Errorf("load current inventory: %w", err)
	}
	byID := make(map[string]model.Resource, len(current))
	for _, r := range current {
		byID[r.ID] = r
	}

	var res Result
	seen := map[string]bool{}
	var inserts []model.Resource
	var closes []string
	// Tri déterministe des ressources observées.
	obs := append([]connector.Resource(nil), snap.Resources...)
	sort.SliceStable(obs, func(i, j int) bool {
		if obs[i].Type != obs[j].Type {
			return obs[i].Type < obs[j].Type
		}
		return obs[i].ExternalID < obs[j].ExternalID
	})
	for _, o := range obs {
		if o.ExternalID == "" || o.Type == "" {
			continue
		}
		id := ids.Resource(org, snap.ConnectorID, o.Type, o.ExternalID)
		if seen[id] {
			continue // doublon dans la même observation
		}
		seen[id] = true
		res.Observed++
		next := model.Resource{
			ID: id, OrgID: org, ConnectorID: snap.ConnectorID, Provider: snap.Provider, Type: o.Type,
			ExternalID: o.ExternalID, Name: o.Name, Region: o.Region,
			Attributes: nonNilAttrs(o.Attributes), Labels: nonNilLabels(o.Labels), ValidFrom: at,
		}
		cur, exists := byID[id]
		switch {
		case !exists:
			// Backfill : une ressource nouvelle existe depuis sa date de création connue.
			if o.CreatedAt != nil && o.CreatedAt.Before(at) {
				next.ValidFrom = o.CreatedAt.UTC().Truncate(time.Second)
				if hist, err := st.Resources().History(ctx, id); err == nil && len(hist) > 0 {
					// Ressource réapparue : on repart de l'observation pour ne pas chevaucher l'historique.
					if last := hist[len(hist)-1]; last.ValidTo != nil && next.ValidFrom.Before(*last.ValidTo) {
						next.ValidFrom = *last.ValidTo
					}
				}
			}
			inserts = append(inserts, next)
			res.Created++
			res.Events = append(res.Events, changeEvent(org, next, "created", next.ValidFrom))
		case changed(cur, next):
			if !at.After(cur.ValidFrom) {
				continue // observation antérieure à la version courante : ignorée
			}
			closes = append(closes, id)
			inserts = append(inserts, next)
			res.Updated++
			ev := changeEvent(org, next, "updated", at)
			ev.Payload["diff"] = diff(cur, next)
			res.Events = append(res.Events, ev)
		}
	}
	res.Events = quietEvents(res.Events)
	if snap.Complete {
		for _, r := range current {
			if !seen[r.ID] {
				closes = append(closes, r.ID)
				res.Deleted++
				res.Events = append(res.Events, changeEvent(org, r, "deleted", at))
			}
		}
	}
	res.Events = quietEvents(res.Events)
	if len(closes) > 0 {
		if err := st.Resources().CloseVersions(ctx, closes, at); err != nil {
			return Result{}, fmt.Errorf("close versions: %w", err)
		}
	}
	if len(inserts) > 0 {
		if err := st.Resources().InsertVersions(ctx, inserts); err != nil {
			return Result{}, fmt.Errorf("insert versions: %w", err)
		}
	}
	if err := applyEdges(ctx, st, org, snap, at, seen); err != nil {
		return Result{}, err
	}
	return res, nil
}

func nonNilAttrs(m map[string]any) map[string]any {
	if m == nil {
		return map[string]any{}
	}
	return m
}

func nonNilLabels(m map[string]string) map[string]string {
	if m == nil {
		return map[string]string{}
	}
	return m
}

// changed indique si la nouvelle observation justifie une nouvelle version.
func changed(cur, next model.Resource) bool {
	if cur.Name != next.Name || cur.Region != next.Region || !reflect.DeepEqual(cur.Labels, next.Labels) {
		return true
	}
	for _, k := range versionedAttributes {
		if cur.Attr(k) != next.Attr(k) {
			return true
		}
	}
	return false
}

// diff décrit les champs modifiés entre deux versions.
func diff(cur, next model.Resource) map[string]any {
	out := map[string]any{}
	if cur.Name != next.Name {
		out["name"] = []string{cur.Name, next.Name}
	}
	for _, k := range versionedAttributes {
		if a, b := cur.Attr(k), next.Attr(k); a != b {
			out["attr."+k] = []string{a, b}
		}
	}
	for k, v := range next.Labels {
		if cur.Labels[k] != v {
			out["label."+k] = []string{cur.Labels[k], v}
		}
	}
	for k, v := range cur.Labels {
		if _, ok := next.Labels[k]; !ok {
			out["label."+k] = []string{v, ""}
		}
	}
	return out
}

// noisyTypes n'émettent pas d'événement de changement : les pods naissent et
// meurent en continu (déploiements, HPA) et noieraient la corrélation. Les
// changements utiles sont portés par leur workload.
var noisyTypes = map[string]bool{model.TypeK8sPod: true}

func changeEvent(org string, r model.Resource, action string, at time.Time) model.Event {
	return model.Event{
		OrgID: org, TS: at, Kind: model.EventInventoryChange, Source: "inventory", ResourceID: r.ID,
		Title: fmt.Sprintf("%s %s %s", r.Type, r.Name, action),
		Payload: map[string]any{
			"action": action, "type": r.Type, "name": r.Name, "external_id": r.ExternalID,
			"flavor": r.Attr("flavor"), "size_gb": r.Attr("size_gb"),
		},
	}
}

type edgeKey struct{ parent, child, rel string }

// applyEdges historise les arêtes déclarées par les ressources observées.
func applyEdges(ctx context.Context, st store.Store, org string, snap Snapshot, at time.Time, seen map[string]bool) error {
	want := map[edgeKey]bool{}
	for _, o := range snap.Resources {
		child := ids.Resource(org, snap.ConnectorID, o.Type, o.ExternalID)
		for _, p := range o.Parents {
			if p.ParentExternalID == "" || p.ParentType == "" {
				continue
			}
			parent := ids.Resource(org, snap.ConnectorID, p.ParentType, p.ParentExternalID)
			want[edgeKey{parent, child, p.Relation}] = true
		}
	}
	cur, err := st.Resources().CurrentEdges(ctx, snap.ConnectorID)
	if err != nil {
		return fmt.Errorf("load edges: %w", err)
	}
	have := map[edgeKey]model.ResourceEdge{}
	for _, e := range cur {
		if e.Relation == model.RelBacks {
			continue // arêtes inter-connecteurs gérées par Link
		}
		have[edgeKey{e.ParentID, e.ChildID, e.Relation}] = e
	}
	var add []model.ResourceEdge
	for k := range want {
		if _, ok := have[k]; !ok {
			add = append(add, model.ResourceEdge{OrgID: org, ParentID: k.parent, ChildID: k.child, Relation: k.rel, ValidFrom: at})
		}
	}
	var remove []model.ResourceEdge
	for k, e := range have {
		if want[k] {
			continue
		}
		// Sans observation complète, seules les arêtes d'enfants réobservés sont révisées.
		if snap.Complete || seen[k.child] {
			remove = append(remove, e)
		}
	}
	sort.Slice(add, func(i, j int) bool {
		if add[i].ParentID != add[j].ParentID {
			return add[i].ParentID < add[j].ParentID
		}
		if add[i].ChildID != add[j].ChildID {
			return add[i].ChildID < add[j].ChildID
		}
		return add[i].Relation < add[j].Relation
	})
	if len(remove) > 0 {
		if err := st.Resources().CloseEdges(ctx, remove, at); err != nil {
			return fmt.Errorf("close edges: %w", err)
		}
	}
	if len(add) > 0 {
		if err := st.Resources().InsertEdges(ctx, add); err != nil {
			return fmt.Errorf("insert edges: %w", err)
		}
	}
	return nil
}

// ResolveID calcule l'identifiant Kairn d'une ressource externe.
func ResolveID(org, connectorID, typ, externalID string) string {
	return ids.Resource(org, connectorID, typ, externalID)
}

// Link crée les arêtes inter-connecteurs : une VM cloud qui porte un node
// Kubernetes (attribut provider_instance_id du node = identifiant externe de la VM).
func Link(ctx context.Context, st store.Store, at time.Time) (int, error) {
	org, err := tenancy.OrgID(ctx)
	if err != nil {
		return 0, err
	}
	at = at.UTC().Truncate(time.Second)
	nodes, err := st.Resources().Current(ctx, store.ResourceFilter{Types: []string{model.TypeK8sNode}})
	if err != nil {
		return 0, err
	}
	vms, err := st.Resources().Current(ctx, store.ResourceFilter{Types: []string{model.TypeInstance}})
	if err != nil {
		return 0, err
	}
	vmByExt := map[string]string{}
	for _, v := range vms {
		vmByExt[v.ExternalID] = v.ID
	}
	edges, err := st.Resources().Edges(ctx, time.Time{}, time.Time{})
	if err != nil {
		return 0, err
	}
	have := map[edgeKey]model.ResourceEdge{}
	for _, e := range edges {
		if e.Relation == model.RelBacks {
			have[edgeKey{e.ParentID, e.ChildID, e.Relation}] = e
		}
	}
	want := map[edgeKey]bool{}
	for _, n := range nodes {
		ext := n.Attr("provider_instance_id")
		if ext == "" {
			continue
		}
		if vm, ok := vmByExt[ext]; ok {
			want[edgeKey{vm, n.ID, model.RelBacks}] = true
		}
	}
	var add, remove []model.ResourceEdge
	for k := range want {
		if _, ok := have[k]; !ok {
			add = append(add, model.ResourceEdge{OrgID: org, ParentID: k.parent, ChildID: k.child, Relation: k.rel, ValidFrom: at})
		}
	}
	for k, e := range have {
		if !want[k] {
			remove = append(remove, e)
		}
	}
	sort.Slice(add, func(i, j int) bool { return add[i].ChildID < add[j].ChildID })
	if len(remove) > 0 {
		if err := st.Resources().CloseEdges(ctx, remove, at); err != nil {
			return 0, err
		}
	}
	if len(add) > 0 {
		if err := st.Resources().InsertEdges(ctx, add); err != nil {
			return 0, err
		}
	}
	return len(add), nil
}

func quietEvents(evs []model.Event) []model.Event {
	out := evs[:0]
	for _, e := range evs {
		if t, _ := e.Payload["type"].(string); !noisyTypes[t] {
			out = append(out, e)
		}
	}
	return out
}
