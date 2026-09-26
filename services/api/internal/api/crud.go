package api

import (
	"context"
	"net/http"

	"github.com/danielgtaylor/huma/v2"

	"github.com/kairn-io/kairn/pkg/auth"
	"github.com/kairn-io/kairn/pkg/plans"
	"github.com/kairn-io/kairn/pkg/store"
)

// OrgPath est le paramètre d'organisation commun.
type OrgPath struct {
	OrgID string `path:"org_id" doc:"Identifiant de l'organisation"`
}

// IDPath identifie une entité dans l'organisation.
type IDPath struct {
	OrgID string `path:"org_id"`
	ID    string `path:"id"`
}

// ListParams sont les paramètres de pagination communs.
type ListParams struct {
	OrgID  string `path:"org_id"`
	Cursor string `query:"cursor" doc:"Curseur renvoyé par la page précédente"`
	Limit  int    `query:"limit" minimum:"0" maximum:"500" default:"50"`
	Kind   string `query:"kind" doc:"Filtre sur le type (si applicable)"`
	Status string `query:"status" doc:"Filtre sur le statut (si applicable)"`
	NodeID string `query:"node_id" doc:"Filtre sur le nœud d'allocation (si applicable)"`
}

// CreateIn est l'entrée d'une création dans une organisation.
type CreateIn[B any] struct {
	OrgID string `path:"org_id"`
	Body  B
}

// UpdateIn est l'entrée d'un remplacement dans une organisation.
type UpdateIn[B any] struct {
	OrgID string `path:"org_id"`
	ID    string `path:"id"`
	Body  B
}

// crudSpec décrit une ressource CRUD standard d'organisation.
type crudSpec[T any, B any] struct {
	Tag     string
	Path    string // relatif à /orgs/{org_id}, ex. "/allocation/rules"
	Name    string // singulier, ex. "allocation-rule"
	Plural  string // ex. "allocation-rules"
	Summary string // libellé humain au pluriel
	Read    auth.Permission
	Write   auth.Permission
	Feature plans.Feature
	Repo    func() store.CRUD[T]
	ID      func(*T) string
	// Apply valide le corps et l'applique à l'entité (création si isNew).
	Apply func(ctx context.Context, a access, body *B, v *T, isNew bool) error
	// Filters liste les filtres de ListParams pris en charge (kind, status, node_id).
	Filters []string
	Redact  func(*T)
	// After est appelé après une écriture réussie (op = create | update | delete).
	After func(ctx context.Context, a access, v *T, op string)
}

func registerCRUD[T any, B any](s *Server, sp crudSpec[T, B]) {
	base := Prefix + "/orgs/{org_id}" + sp.Path
	redact := func(v *T) {
		if sp.Redact != nil {
			sp.Redact(v)
		}
	}
	huma.Register(s.API, huma.Operation{
		OperationID: "list-" + sp.Plural, Method: http.MethodGet, Path: base, Tags: []string{sp.Tag},
		Summary: "Liste des " + sp.Summary,
	}, func(ctx context.Context, in *ListParams) (*Out[Page[T]], error) {
		ctx, _, err := s.enter(ctx, in.OrgID, sp.Read, sp.Feature)
		if err != nil {
			return nil, err
		}
		filters := map[string]string{}
		for _, f := range sp.Filters {
			switch {
			case f == "kind" && in.Kind != "":
				filters["kind"] = in.Kind
			case f == "status" && in.Status != "":
				filters["status"] = in.Status
			case f == "node_id" && in.NodeID != "":
				filters["node_id"] = in.NodeID
			}
		}
		q := store.ListQuery{Cursor: in.Cursor, Limit: in.Limit, Filters: filters}.Normalize()
		items, err := sp.Repo().List(ctx, q)
		if err != nil {
			return nil, s.fail(ctx, err)
		}
		for i := range items {
			redact(&items[i])
		}
		return out(page(items, q.Limit, func(v T) string { return sp.ID(&v) })), nil
	})
	huma.Register(s.API, huma.Operation{
		OperationID: "get-" + sp.Name, Method: http.MethodGet, Path: base + "/{id}", Tags: []string{sp.Tag},
		Summary: "Détail",
	}, func(ctx context.Context, in *IDPath) (*Out[T], error) {
		ctx, _, err := s.enter(ctx, in.OrgID, sp.Read, sp.Feature)
		if err != nil {
			return nil, err
		}
		v, err := sp.Repo().Get(ctx, in.ID)
		if err != nil {
			return nil, s.fail(ctx, err)
		}
		redact(&v)
		return out(v), nil
	})
	huma.Register(s.API, huma.Operation{
		OperationID: "create-" + sp.Name, Method: http.MethodPost, Path: base, Tags: []string{sp.Tag},
		Summary: "Création", DefaultStatus: http.StatusCreated,
	}, func(ctx context.Context, in *CreateIn[B]) (*Out[T], error) {
		ctx, a, err := s.enter(ctx, in.OrgID, sp.Write, sp.Feature)
		if err != nil {
			return nil, err
		}
		var v T
		if err := sp.Apply(ctx, a, &in.Body, &v, true); err != nil {
			return nil, s.fail(ctx, err)
		}
		if err := sp.Repo().Create(ctx, &v); err != nil {
			return nil, s.fail(ctx, err)
		}
		s.audit(ctx, a, sp.Name+".create", sp.Name, sp.ID(&v), nil)
		if sp.After != nil {
			sp.After(ctx, a, &v, "create")
		}
		redact(&v)
		return out(v), nil
	})
	huma.Register(s.API, huma.Operation{
		OperationID: "update-" + sp.Name, Method: http.MethodPut, Path: base + "/{id}", Tags: []string{sp.Tag},
		Summary: "Remplacement",
	}, func(ctx context.Context, in *UpdateIn[B]) (*Out[T], error) {
		ctx, a, err := s.enter(ctx, in.OrgID, sp.Write, sp.Feature)
		if err != nil {
			return nil, err
		}
		v, err := sp.Repo().Get(ctx, in.ID)
		if err != nil {
			return nil, s.fail(ctx, err)
		}
		if err := sp.Apply(ctx, a, &in.Body, &v, false); err != nil {
			return nil, s.fail(ctx, err)
		}
		if err := sp.Repo().Update(ctx, &v); err != nil {
			return nil, s.fail(ctx, err)
		}
		s.audit(ctx, a, sp.Name+".update", sp.Name, in.ID, nil)
		if sp.After != nil {
			sp.After(ctx, a, &v, "update")
		}
		redact(&v)
		return out(v), nil
	})
	huma.Register(s.API, huma.Operation{
		OperationID: "delete-" + sp.Name, Method: http.MethodDelete, Path: base + "/{id}", Tags: []string{sp.Tag},
		Summary: "Suppression", DefaultStatus: http.StatusNoContent,
	}, func(ctx context.Context, in *IDPath) (*Empty, error) {
		ctx, a, err := s.enter(ctx, in.OrgID, sp.Write, sp.Feature)
		if err != nil {
			return nil, err
		}
		v, err := sp.Repo().Get(ctx, in.ID)
		if err != nil {
			return nil, s.fail(ctx, err)
		}
		if err := sp.Repo().Delete(ctx, in.ID); err != nil {
			return nil, s.fail(ctx, err)
		}
		s.audit(ctx, a, sp.Name+".delete", sp.Name, in.ID, nil)
		if sp.After != nil {
			sp.After(ctx, a, &v, "delete")
		}
		return &Empty{}, nil
	})
}
