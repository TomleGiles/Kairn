// Package tenancy porte l'organisation courante dans le contexte. Toute
// lecture ou écriture de donnée client passe par OrgID(ctx) : une requête
// sans organisation échoue, jamais elle ne renvoie les données de toutes les
// organisations.
package tenancy

import (
	"context"
	"errors"
	"fmt"
)

// ErrNoOrg est renvoyée quand aucune organisation n'est présente dans le contexte.
var ErrNoOrg = errors.New("tenancy: no organization in context")

// ErrCrossOrg est renvoyée quand une entité n'appartient pas à l'organisation courante.
var ErrCrossOrg = errors.New("tenancy: cross-organization access denied")

type orgKey struct{}
type userKey struct{}
type systemKey struct{}

// WithOrg attache l'organisation courante au contexte.
func WithOrg(ctx context.Context, orgID string) context.Context {
	return context.WithValue(ctx, orgKey{}, orgID)
}

// OrgID renvoie l'organisation courante ou ErrNoOrg.
func OrgID(ctx context.Context) (string, error) {
	v, _ := ctx.Value(orgKey{}).(string)
	if v == "" {
		return "", ErrNoOrg
	}
	return v, nil
}

// Check vérifie qu'une entité appartient bien à l'organisation courante.
func Check(ctx context.Context, entityOrgID string) error {
	org, err := OrgID(ctx)
	if err != nil {
		return err
	}
	if org != entityOrgID {
		return fmt.Errorf("%w: entity belongs to another organization", ErrCrossOrg)
	}
	return nil
}

// WithSystem marque le contexte comme opération système (ex. ordonnanceur
// qui énumère les organisations). Les dépôts n'autorisent via ce marqueur
// que les opérations explicitement prévues (liste des identifiants d'org).
func WithSystem(ctx context.Context) context.Context {
	return context.WithValue(ctx, systemKey{}, true)
}

// IsSystem indique si le contexte est une opération système.
func IsSystem(ctx context.Context) bool {
	v, _ := ctx.Value(systemKey{}).(bool)
	return v
}

// WithUser attache l'utilisateur courant (utilisé par la RLS pour les
// lectures transverses propres à l'utilisateur : ses organisations, son profil).
func WithUser(ctx context.Context, userID string) context.Context {
	return context.WithValue(ctx, userKey{}, userID)
}

// UserID renvoie l'utilisateur courant, ou "".
func UserID(ctx context.Context) string {
	v, _ := ctx.Value(userKey{}).(string)
	return v
}
