package inventory

import (
	"context"
	"testing"
	"time"

	"github.com/kairn-io/kairn/pkg/connector"
	"github.com/kairn-io/kairn/pkg/model"
	"github.com/kairn-io/kairn/pkg/store"
	"github.com/kairn-io/kairn/pkg/store/memstore"
	"github.com/kairn-io/kairn/pkg/tenancy"
)

var t0 = time.Date(2026, 9, 1, 12, 0, 0, 0, time.UTC)

func server(ext, flavor string, parents ...connector.Edge) connector.Resource {
	return connector.Resource{Type: model.TypeInstance, ExternalID: ext, Name: ext, Region: "GRA11",
		Attributes: map[string]any{"flavor": flavor}, Labels: map[string]string{"team": "shop"}, Parents: parents}
}

func TestApplyLifecycle(t *testing.T) {
	ctx := tenancy.WithOrg(context.Background(), "org-a")
	st := memstore.New()
	proj := connector.Resource{Type: model.TypeProject, ExternalID: "p1", Name: "prod"}
	inProj := connector.Edge{Relation: model.RelContains, ParentType: model.TypeProject, ParentExternalID: "p1"}

	// 1. Création.
	r, err := Apply(ctx, st, Snapshot{ConnectorID: "c1", Provider: "openstack", ObservedAt: t0, Complete: true,
		Resources: []connector.Resource{proj, server("vm1", "b2-7", inProj), server("vm2", "b2-7", inProj)}})
	if err != nil {
		t.Fatal(err)
	}
	if r.Created != 3 || r.Updated != 0 || r.Deleted != 0 {
		t.Fatalf("first sync: %+v", r)
	}
	// 2. Réapplication identique : idempotente.
	r, _ = Apply(ctx, st, Snapshot{ConnectorID: "c1", Provider: "openstack", ObservedAt: t0.Add(time.Hour), Complete: true,
		Resources: []connector.Resource{proj, server("vm1", "b2-7", inProj), server("vm2", "b2-7", inProj)}})
	if r.Created+r.Updated+r.Deleted != 0 {
		t.Fatalf("replay must be a no-op: %+v", r)
	}
	// 3. Redimensionnement de vm1, disparition de vm2.
	t2 := t0.Add(2 * time.Hour)
	r, _ = Apply(ctx, st, Snapshot{ConnectorID: "c1", Provider: "openstack", ObservedAt: t2, Complete: true,
		Resources: []connector.Resource{proj, server("vm1", "b2-15", inProj)}})
	if r.Updated != 1 || r.Deleted != 1 {
		t.Fatalf("resize+delete: %+v", r)
	}
	vm1 := ResolveID("org-a", "c1", model.TypeInstance, "vm1")
	hist, err := st.Resources().History(ctx, vm1)
	if err != nil || len(hist) != 2 {
		t.Fatalf("vm1 history: %v %+v", err, hist)
	}
	if hist[0].ValidTo == nil || !hist[0].ValidTo.Equal(t2) || hist[1].Attr("flavor") != "b2-15" {
		t.Fatalf("versions: %+v", hist)
	}
	// Instantané historique : à t0+1h vm2 existait.
	past, _ := st.Resources().Current(ctx, store.ResourceFilter{At: t0.Add(time.Hour), Types: []string{model.TypeInstance}})
	if len(past) != 2 {
		t.Fatalf("as-of query: %d", len(past))
	}
	now, _ := st.Resources().Current(ctx, store.ResourceFilter{Types: []string{model.TypeInstance}})
	if len(now) != 1 {
		t.Fatalf("current: %d", len(now))
	}
	// Arête projet → vm2 fermée.
	edges, _ := st.Resources().Edges(ctx, time.Time{}, time.Time{})
	if len(edges) != 1 {
		t.Fatalf("current edges: %+v", edges)
	}
}

func TestPartialSyncDoesNotDelete(t *testing.T) {
	ctx := tenancy.WithOrg(context.Background(), "org-a")
	st := memstore.New()
	Apply(ctx, st, Snapshot{ConnectorID: "c1", ObservedAt: t0, Complete: true,
		Resources: []connector.Resource{server("vm1", "b2-7"), server("vm2", "b2-7")}})
	r, _ := Apply(ctx, st, Snapshot{ConnectorID: "c1", ObservedAt: t0.Add(time.Hour), Complete: false,
		Resources: []connector.Resource{server("vm1", "b2-7")}})
	if r.Deleted != 0 {
		t.Fatalf("an incomplete sync must never delete: %+v", r)
	}
}

func TestBackfillUsesCreationDate(t *testing.T) {
	ctx := tenancy.WithOrg(context.Background(), "org-a")
	st := memstore.New()
	created := t0.AddDate(0, -3, 0)
	s := server("vm1", "b2-7")
	s.CreatedAt = &created
	Apply(ctx, st, Snapshot{ConnectorID: "c1", ObservedAt: t0, Complete: true, Resources: []connector.Resource{s}})
	got, _ := st.Resources().Get(ctx, ResolveID("org-a", "c1", model.TypeInstance, "vm1"))
	if !got.ValidFrom.Equal(created) {
		t.Fatalf("valid_from should be the provider creation date, got %s", got.ValidFrom)
	}
}

func TestTenantIsolation(t *testing.T) {
	st := memstore.New()
	a := tenancy.WithOrg(context.Background(), "org-a")
	b := tenancy.WithOrg(context.Background(), "org-b")
	Apply(a, st, Snapshot{ConnectorID: "c1", ObservedAt: t0, Complete: true, Resources: []connector.Resource{server("vm1", "b2-7")}})
	got, _ := st.Resources().Current(b, store.ResourceFilter{})
	if len(got) != 0 {
		t.Fatalf("org-b must not see org-a resources")
	}
	if _, err := Apply(context.Background(), st, Snapshot{ConnectorID: "c1", ObservedAt: t0}); err == nil {
		t.Fatal("apply without org must fail")
	}
}

func TestLinkNodesToVMs(t *testing.T) {
	ctx := tenancy.WithOrg(context.Background(), "org-a")
	st := memstore.New()
	Apply(ctx, st, Snapshot{ConnectorID: "os", ObservedAt: t0, Complete: true, Resources: []connector.Resource{server("uuid-1", "b2-15")}})
	Apply(ctx, st, Snapshot{ConnectorID: "k8s", ObservedAt: t0, Complete: true, Resources: []connector.Resource{
		{Type: model.TypeK8sNode, ExternalID: "node-1", Attributes: map[string]any{"provider_instance_id": "uuid-1"}},
	}})
	n, err := Link(ctx, st, t0)
	if err != nil || n != 1 {
		t.Fatalf("link: %d %v", n, err)
	}
	// Une resynchronisation K8s ne doit pas fermer l'arête inter-connecteurs.
	Apply(ctx, st, Snapshot{ConnectorID: "k8s", ObservedAt: t0.Add(time.Hour), Complete: true, Resources: []connector.Resource{
		{Type: model.TypeK8sNode, ExternalID: "node-1", Attributes: map[string]any{"provider_instance_id": "uuid-1"}},
	}})
	edges, _ := st.Resources().Edges(ctx, time.Time{}, time.Time{})
	if len(edges) != 1 || edges[0].Relation != model.RelBacks {
		t.Fatalf("backs edge must survive: %+v", edges)
	}
	if n, _ := Link(ctx, st, t0.Add(2*time.Hour)); n != 0 {
		t.Fatal("link is idempotent")
	}
}
