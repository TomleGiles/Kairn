// Package auth gère les identités (sessions, OIDC, jetons d'API), les
// permissions RBAC (M-11) et leur évaluation.
package auth

import (
	"slices"

	"github.com/kairn-io/kairn/pkg/model"
)

// Permission est une action autorisable.
type Permission string

// Permissions.
const (
	PermOrgRead          Permission = "org:read"
	PermOrgManage        Permission = "org:manage"
	PermMembersManage    Permission = "members:manage"
	PermBillingManage    Permission = "billing:manage"
	PermConnectorsRead   Permission = "connectors:read"
	PermConnectorsManage Permission = "connectors:manage"
	PermCostsRead        Permission = "costs:read"
	PermPricingManage    Permission = "pricing:manage"
	PermAllocationManage Permission = "allocation:manage"
	PermBudgetsManage    Permission = "budgets:manage"
	PermRecoManage       Permission = "recommendations:manage"
	PermAlertsManage     Permission = "alerts:manage"
	PermUptimeManage     Permission = "uptime:manage"
	PermReportsRead      Permission = "reports:read"
	PermReportsManage    Permission = "reports:manage"
	PermAuditRead        Permission = "audit:read"
	PermTokensManage     Permission = "tokens:manage"
	PermAssistantUse     Permission = "assistant:use"
	PermExport           Permission = "export"
	PermSCIM             Permission = "scim"
)

// AllPermissions liste toutes les permissions (documentation, jetons).
var AllPermissions = []Permission{
	PermOrgRead, PermOrgManage, PermMembersManage, PermBillingManage, PermConnectorsRead, PermConnectorsManage,
	PermCostsRead, PermPricingManage, PermAllocationManage, PermBudgetsManage, PermRecoManage, PermAlertsManage,
	PermUptimeManage, PermReportsRead, PermReportsManage, PermAuditRead, PermTokensManage, PermAssistantUse,
	PermExport, PermSCIM,
}

var readOnly = []Permission{PermOrgRead, PermConnectorsRead, PermCostsRead, PermReportsRead}

var rolePermissions = map[model.Role][]Permission{
	model.RoleOwner: AllPermissions,
	model.RoleAdmin: without(AllPermissions, PermBillingManage),
	model.RoleFinance: append(slices.Clone(readOnly),
		PermPricingManage, PermAllocationManage, PermBudgetsManage, PermAlertsManage, PermReportsManage,
		PermAssistantUse, PermExport),
	model.RoleEngineer: append(slices.Clone(readOnly),
		PermRecoManage, PermAlertsManage, PermUptimeManage, PermAssistantUse, PermExport),
	model.RoleViewer: append(slices.Clone(readOnly), PermAssistantUse),
}

func without(list []Permission, drop ...Permission) []Permission {
	var out []Permission
	for _, p := range list {
		if !slices.Contains(drop, p) {
			out = append(out, p)
		}
	}
	return out
}

// RoleCan indique si un rôle accorde une permission. SCIM n'est accordé qu'aux jetons dédiés.
func RoleCan(r model.Role, p Permission) bool {
	return slices.Contains(rolePermissions[r], p)
}

// RolePermissions renvoie les permissions d'un rôle.
func RolePermissions(r model.Role) []Permission {
	return slices.Clone(rolePermissions[r])
}

// RoleRank ordonne les rôles (plus petit = plus privilégié).
func RoleRank(r model.Role) int {
	for i, x := range model.Roles {
		if x == r {
			return i
		}
	}
	return len(model.Roles)
}

// ValidScopes vérifie que des scopes de jeton sont des permissions connues.
func ValidScopes(scopes []string) bool {
	for _, s := range scopes {
		if !slices.Contains(AllPermissions, Permission(s)) {
			return false
		}
	}
	return true
}
