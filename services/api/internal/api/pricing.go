package api

import (
	"context"
	"net/http"
	"strings"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/shopspring/decimal"

	"github.com/kairn-io/kairn/pkg/auth"
	"github.com/kairn-io/kairn/pkg/costengine"
	"github.com/kairn-io/kairn/pkg/model"
	"github.com/kairn-io/kairn/pkg/pricing/catalogs"
	"github.com/kairn-io/kairn/pkg/store"
)

type onPremBody struct {
	Name               string            `json:"name" required:"true" minLength:"1" maxLength:"120"`
	ConnectorID        *string           `json:"connector_id,omitempty"`
	Selector           map[string]string `json:"selector,omitempty" doc:"Labels identifiant les ressources couvertes"`
	Currency           string            `json:"currency,omitempty" enum:"EUR,USD,GBP,CHF"`
	HardwareCost       decimal.Decimal   `json:"hardware_cost"`
	AmortizationMonths int               `json:"amortization_months" minimum:"1" maximum:"120" required:"true"`
	PowerKW            decimal.Decimal   `json:"power_kw"`
	PowerPricePerKWh   decimal.Decimal   `json:"power_price_per_kwh"`
	PUE                decimal.Decimal   `json:"pue"`
	LicensesMonthly    decimal.Decimal   `json:"licenses_monthly"`
	LaborMonthly       decimal.Decimal   `json:"labor_monthly"`
	OtherMonthly       decimal.Decimal   `json:"other_monthly"`
	CapacityVCPU       decimal.Decimal   `json:"capacity_vcpu"`
	CapacityRAMGB      decimal.Decimal   `json:"capacity_ram_gb"`
	CapacityStorageGB  decimal.Decimal   `json:"capacity_storage_gb"`
	WeightCPU          decimal.Decimal   `json:"weight_cpu"`
	WeightRAM          decimal.Decimal   `json:"weight_ram"`
	WeightStorage      decimal.Decimal   `json:"weight_storage"`
	ValidFrom          time.Time         `json:"valid_from" required:"true"`
	ValidTo            *time.Time        `json:"valid_to,omitempty"`
}

type adjustmentBody struct {
	Kind         string           `json:"kind" required:"true" enum:"discount,commitment,credit"`
	Name         string           `json:"name" required:"true" minLength:"1" maxLength:"120"`
	Provider     string           `json:"provider,omitempty"`
	SKUPattern   string           `json:"sku_pattern,omitempty" doc:"Motif glob, ex. compute.flavor.*"`
	Percent      *decimal.Decimal `json:"percent,omitempty"`
	Amount       *decimal.Decimal `json:"amount,omitempty"`
	CoveredUnits *decimal.Decimal `json:"covered_units,omitempty"`
	Currency     string           `json:"currency,omitempty" enum:"EUR,USD,GBP,CHF"`
	ValidFrom    time.Time        `json:"valid_from" required:"true"`
	ValidTo      *time.Time       `json:"valid_to,omitempty"`
}

// recomputeAfterPricing relance le calcul des 60 derniers jours après un changement tarifaire.
func (s *Server) recomputeRecent(ctx context.Context, a access) {
	if s.Jobs == nil {
		return
	}
	to := s.now().AddDate(0, 0, 1)
	if err := s.Jobs.RequestRecompute(ctx, a.Org.ID, to.AddDate(0, 0, -60), to); err != nil {
		s.Log.Warn("recompute request failed", "org", a.Org.ID, "err", err)
	}
}

func nonNeg(vals ...decimal.Decimal) bool {
	for _, v := range vals {
		if v.IsNegative() {
			return false
		}
	}
	return true
}

func (s *Server) registerPricing() {
	tag := "Tarification"
	huma.Register(s.API, huma.Operation{OperationID: "list-price-catalogs", Method: http.MethodGet, Path: Prefix + "/orgs/{org_id}/pricing/catalogs", Tags: []string{tag},
		Summary: "Grilles tarifaires publiques et négociées, versionnées"},
		func(ctx context.Context, in *OrgPath) (*Out[[]model.PriceCatalog], error) {
			ctx, _, err := s.enter(ctx, in.OrgID, auth.PermCostsRead, "")
			if err != nil {
				return nil, err
			}
			cats, err := s.Store.Pricing().ListCatalogs(ctx)
			if err != nil {
				return nil, s.fail(ctx, err)
			}
			if cats == nil {
				cats = []model.PriceCatalog{}
			}
			return out(cats), nil
		})
	huma.Register(s.API, huma.Operation{OperationID: "list-price-items", Method: http.MethodGet, Path: Prefix + "/orgs/{org_id}/pricing/catalogs/{id}/items", Tags: []string{tag},
		Summary: "Articles d'une grille"},
		func(ctx context.Context, in *IDPath) (*Out[[]model.PriceItem], error) {
			ctx, _, err := s.enter(ctx, in.OrgID, auth.PermCostsRead, "")
			if err != nil {
				return nil, err
			}
			items, err := s.Store.Pricing().Items(ctx, in.ID)
			if err != nil {
				return nil, s.fail(ctx, err)
			}
			return out(items), nil
		})
	huma.Register(s.API, huma.Operation{OperationID: "import-price-catalog", Method: http.MethodPost, Path: Prefix + "/orgs/{org_id}/pricing/catalogs", Tags: []string{tag},
		Summary: "Importe une grille négociée (prioritaire sur la grille publique)", DefaultStatus: http.StatusCreated},
		func(ctx context.Context, in *CreateIn[catalogs.File]) (*Out[model.PriceCatalog], error) {
			ctx, a, err := s.enter(ctx, in.OrgID, auth.PermPricingManage, "")
			if err != nil {
				return nil, err
			}
			c, items, err := in.Body.ToCatalog()
			if err != nil {
				return nil, invalid(err.Error())
			}
			org := a.Org.ID
			c.OrgID = &org
			if c.Source == "" {
				c.Source = "negotiated"
			}
			if err := s.Store.Pricing().CreateCatalog(ctx, &c, items); err != nil {
				return nil, s.fail(ctx, err)
			}
			s.audit(ctx, a, "pricing.catalog.import", "price_catalog", c.ID, map[string]any{"provider": c.Provider, "version": c.Version, "items": len(items)})
			s.recomputeRecent(ctx, a)
			return out(c), nil
		})
	huma.Register(s.API, huma.Operation{OperationID: "delete-price-catalog", Method: http.MethodDelete, Path: Prefix + "/orgs/{org_id}/pricing/catalogs/{id}", Tags: []string{tag},
		Summary: "Supprime une grille négociée", DefaultStatus: http.StatusNoContent},
		func(ctx context.Context, in *IDPath) (*Empty, error) {
			ctx, a, err := s.enter(ctx, in.OrgID, auth.PermPricingManage, "")
			if err != nil {
				return nil, err
			}
			if err := s.Store.Pricing().DeleteCatalog(ctx, in.ID); err != nil {
				return nil, s.fail(ctx, err)
			}
			s.audit(ctx, a, "pricing.catalog.delete", "price_catalog", in.ID, nil)
			s.recomputeRecent(ctx, a)
			return &Empty{}, nil
		})

	registerCRUD(s, crudSpec[model.OnPremCostModel, onPremBody]{
		Tag: tag, Path: "/pricing/onprem-models", Name: "onprem-model", Plural: "onprem-models", Summary: "modèles de coût on-prem",
		Read: auth.PermCostsRead, Write: auth.PermPricingManage, Repo: func() store.CRUD[model.OnPremCostModel] { return s.Store.OnPremModels() },
		ID: func(m *model.OnPremCostModel) string { return m.ID },
		Apply: func(ctx context.Context, a access, b *onPremBody, m *model.OnPremCostModel, isNew bool) error {
			if !nonNeg(b.HardwareCost, b.PowerKW, b.PowerPricePerKWh, b.LicensesMonthly, b.LaborMonthly, b.OtherMonthly,
				b.CapacityVCPU, b.CapacityRAMGB, b.CapacityStorageGB, b.WeightCPU, b.WeightRAM, b.WeightStorage) {
				return invalid("amounts, capacities and weights must be positive")
			}
			if b.ConnectorID == nil && len(b.Selector) == 0 {
				return invalid("connector_id or selector is required")
			}
			if b.ValidTo != nil && !b.ValidTo.After(b.ValidFrom) {
				return invalid("valid_to must be after valid_from")
			}
			*m = model.OnPremCostModel{
				ID: m.ID, OrgID: a.Org.ID, Name: b.Name, ConnectorID: b.ConnectorID, Selector: b.Selector, Currency: b.Currency,
				HardwareCost: b.HardwareCost, AmortizationMonths: b.AmortizationMonths, PowerKW: b.PowerKW, PowerPricePerKWh: b.PowerPricePerKWh,
				PUE: b.PUE, LicensesMonthly: b.LicensesMonthly, LaborMonthly: b.LaborMonthly, OtherMonthly: b.OtherMonthly,
				CapacityVCPU: b.CapacityVCPU, CapacityRAMGB: b.CapacityRAMGB, CapacityStorageGB: b.CapacityStorageGB,
				WeightCPU: b.WeightCPU, WeightRAM: b.WeightRAM, WeightStorage: b.WeightStorage, ValidFrom: b.ValidFrom, ValidTo: b.ValidTo, CreatedAt: m.CreatedAt,
			}
			if m.Currency == "" {
				m.Currency = a.Org.Currency
			}
			if m.PUE.IsZero() {
				m.PUE = decimal.RequireFromString("1.5")
			}
			if m.WeightCPU.IsZero() && m.WeightRAM.IsZero() && m.WeightStorage.IsZero() {
				m.WeightCPU, m.WeightRAM, m.WeightStorage = decimal.RequireFromString("0.5"), decimal.RequireFromString("0.4"), decimal.RequireFromString("0.1")
			}
			if m.Selector == nil {
				m.Selector = map[string]string{}
			}
			return nil
		},
		After: func(ctx context.Context, a access, _ *model.OnPremCostModel, _ string) { s.recomputeRecent(ctx, a) },
	})

	type ratesOut struct {
		VCPUHour       decimal.Decimal `json:"vcpu_hour"`
		RAMGBHour      decimal.Decimal `json:"ram_gb_hour"`
		StorageGBMonth decimal.Decimal `json:"storage_gb_month"`
		Currency       string          `json:"currency"`
	}
	huma.Register(s.API, huma.Operation{OperationID: "get-onprem-rates", Method: http.MethodGet, Path: Prefix + "/orgs/{org_id}/pricing/onprem-models/{id}/rates", Tags: []string{tag},
		Summary: "Coûts unitaires dérivés d'un modèle on-prem"},
		func(ctx context.Context, in *IDPath) (*Out[ratesOut], error) {
			ctx, _, err := s.enter(ctx, in.OrgID, auth.PermCostsRead, "")
			if err != nil {
				return nil, err
			}
			m, err := s.Store.OnPremModels().Get(ctx, in.ID)
			if err != nil {
				return nil, s.fail(ctx, err)
			}
			v, r, st := costengine.OnPremRates(m)
			return out(ratesOut{VCPUHour: v.Round(6), RAMGBHour: r.Round(6), StorageGBMonth: st.Round(6), Currency: m.Currency}), nil
		})

	registerCRUD(s, crudSpec[model.PricingAdjustment, adjustmentBody]{
		Tag: tag, Path: "/pricing/adjustments", Name: "pricing-adjustment", Plural: "pricing-adjustments", Summary: "remises, engagements et crédits",
		Read: auth.PermCostsRead, Write: auth.PermPricingManage, Repo: func() store.CRUD[model.PricingAdjustment] { return s.Store.Adjustments() }, Filters: []string{"kind"},
		ID: func(m *model.PricingAdjustment) string { return m.ID },
		Apply: func(ctx context.Context, a access, b *adjustmentBody, m *model.PricingAdjustment, isNew bool) error {
			switch b.Kind {
			case model.AdjustmentDiscount:
				if b.Percent == nil || !b.Percent.IsPositive() || b.Percent.GreaterThan(decimal.NewFromInt(100)) {
					return invalid("discount requires 0 < percent <= 100")
				}
			case model.AdjustmentCommitment:
				if b.Amount == nil || !b.Amount.IsPositive() || b.CoveredUnits == nil || !b.CoveredUnits.IsPositive() || b.SKUPattern == "" {
					return invalid("commitment requires amount, covered_units and sku_pattern")
				}
			case model.AdjustmentCredit:
				if b.Amount == nil || !b.Amount.IsPositive() {
					return invalid("credit requires a positive amount")
				}
			}
			if b.ValidTo != nil && !b.ValidTo.After(b.ValidFrom) {
				return invalid("valid_to must be after valid_from")
			}
			*m = model.PricingAdjustment{ID: m.ID, OrgID: a.Org.ID, Kind: b.Kind, Name: b.Name, Provider: strings.ToLower(b.Provider),
				SKUPattern: b.SKUPattern, Percent: b.Percent, Amount: b.Amount, CoveredUnits: b.CoveredUnits, Currency: b.Currency,
				ValidFrom: b.ValidFrom.UTC(), ValidTo: b.ValidTo, CreatedAt: m.CreatedAt}
			if m.Currency == "" {
				m.Currency = a.Org.Currency
			}
			return nil
		},
		After: func(ctx context.Context, a access, _ *model.PricingAdjustment, _ string) { s.recomputeRecent(ctx, a) },
	})
}
