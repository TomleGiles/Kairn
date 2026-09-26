package model

import (
	"time"

	"github.com/shopspring/decimal"
)

// Unités tarifaires reconnues par le cost-engine.
const (
	UnitHour     = "hour"      // prix par heure d'existence
	UnitMonth    = "month"     // prix mensuel (730 h)
	UnitGBMonth  = "gb_month"  // prix par Go et par mois
	UnitGBHour   = "gb_hour"   // prix par Go et par heure
	UnitGB       = "gb"        // prix par Go transféré
	UnitRequest  = "request"   // prix par requête
	UnitVCPUHour = "vcpu_hour" // on-prem / fallback
	UnitUnit     = "unit"      // prix forfaitaire
)

// CatalogSourceSample marque les grilles d'exemple embarquées (indicatives) :
// elles ne servent que tant qu'aucune grille publique importée n'est valide.
const CatalogSourceSample = "sample"

// PriceCatalog est une grille tarifaire versionnée. OrgID nul = grille publique.
type PriceCatalog struct {
	ID         string    `json:"id" db:"id"`
	OrgID      *string   `json:"org_id,omitempty" db:"org_id"`
	Provider   string    `json:"provider" db:"provider"`
	Version    string    `json:"version" db:"version"`
	Source     string    `json:"source" db:"source"`
	Currency   string    `json:"currency" db:"currency"`
	ValidFrom  time.Time `json:"valid_from" db:"valid_from"`
	ImportedAt time.Time `json:"imported_at" db:"imported_at"`
}

// PriceItem est un article de grille tarifaire.
type PriceItem struct {
	CatalogID  string            `json:"catalog_id" db:"catalog_id"`
	SKU        string            `json:"sku" db:"sku"`
	Region     string            `json:"region" db:"region"` // "" = toutes régions
	Unit       string            `json:"unit" db:"unit"`
	Price      decimal.Decimal   `json:"price" db:"price"`
	Currency   string            `json:"currency" db:"currency"`
	Attributes map[string]string `json:"attributes" db:"attributes"`
}

// OnPremCostModel ventile un coût on-prem total en coûts unitaires.
type OnPremCostModel struct {
	ID                 string            `json:"id" db:"id"`
	OrgID              string            `json:"org_id" db:"org_id"`
	Name               string            `json:"name" db:"name"`
	ConnectorID        *string           `json:"connector_id,omitempty" db:"connector_id"`
	Selector           map[string]string `json:"selector" db:"selector"` // labels à matcher
	Currency           string            `json:"currency" db:"currency"`
	HardwareCost       decimal.Decimal   `json:"hardware_cost" db:"hardware_cost"`
	AmortizationMonths int               `json:"amortization_months" db:"amortization_months"`
	PowerKW            decimal.Decimal   `json:"power_kw" db:"power_kw"`
	PowerPricePerKWh   decimal.Decimal   `json:"power_price_per_kwh" db:"power_price_per_kwh"`
	PUE                decimal.Decimal   `json:"pue" db:"pue"`
	LicensesMonthly    decimal.Decimal   `json:"licenses_monthly" db:"licenses_monthly"`
	LaborMonthly       decimal.Decimal   `json:"labor_monthly" db:"labor_monthly"`
	OtherMonthly       decimal.Decimal   `json:"other_monthly" db:"other_monthly"`
	CapacityVCPU       decimal.Decimal   `json:"capacity_vcpu" db:"capacity_vcpu"`
	CapacityRAMGB      decimal.Decimal   `json:"capacity_ram_gb" db:"capacity_ram_gb"`
	CapacityStorageGB  decimal.Decimal   `json:"capacity_storage_gb" db:"capacity_storage_gb"`
	WeightCPU          decimal.Decimal   `json:"weight_cpu" db:"weight_cpu"`
	WeightRAM          decimal.Decimal   `json:"weight_ram" db:"weight_ram"`
	WeightStorage      decimal.Decimal   `json:"weight_storage" db:"weight_storage"`
	ValidFrom          time.Time         `json:"valid_from" db:"valid_from"`
	ValidTo            *time.Time        `json:"valid_to,omitempty" db:"valid_to"`
	CreatedAt          time.Time         `json:"created_at" db:"created_at"`
}

// Types d'ajustement tarifaire.
const (
	AdjustmentDiscount   = "discount"   // remise en pourcentage
	AdjustmentCommitment = "commitment" // engagement : forfait mensuel couvrant N unités d'un SKU
	AdjustmentCredit     = "credit"     // crédit consommé jusqu'à épuisement
)

// PricingAdjustment représente une remise, un engagement ou un crédit.
type PricingAdjustment struct {
	ID           string           `json:"id" db:"id"`
	OrgID        string           `json:"org_id" db:"org_id"`
	Kind         string           `json:"kind" db:"kind"`
	Name         string           `json:"name" db:"name"`
	Provider     string           `json:"provider" db:"provider"`       // "" = tous
	SKUPattern   string           `json:"sku_pattern" db:"sku_pattern"` // glob, "" = tous
	Percent      *decimal.Decimal `json:"percent,omitempty" db:"percent"`
	Amount       *decimal.Decimal `json:"amount,omitempty" db:"amount"`
	CoveredUnits *decimal.Decimal `json:"covered_units,omitempty" db:"covered_units"`
	Currency     string           `json:"currency" db:"currency"`
	ValidFrom    time.Time        `json:"valid_from" db:"valid_from"`
	ValidTo      *time.Time       `json:"valid_to,omitempty" db:"valid_to"`
	CreatedAt    time.Time        `json:"created_at" db:"created_at"`
}

// ExchangeRate est un taux de change journalier (quote par unité de base).
type ExchangeRate struct {
	Base  string          `json:"base" db:"base"`
	Quote string          `json:"quote" db:"quote"`
	Day   time.Time       `json:"day" db:"day"`
	Rate  decimal.Decimal `json:"rate" db:"rate"`
}

// Types de coût.
const (
	CostCompute    = "compute"
	CostStorage    = "storage"
	CostNetwork    = "network"
	CostLicense    = "license"
	CostK8sWork    = "k8s_workload"
	CostK8sIdle    = "k8s_idle"
	CostShared     = "shared"
	CostDiscount   = "discount"
	CostCommitment = "commitment"
	CostCredit     = "credit"
	CostTax        = "tax"
	CostOther      = "other"
)

// Sources d'une ligne de coût.
const (
	SourceEstimate = "estimate" // grille tarifaire publique ou négociée
	SourceInvoice  = "invoice"  // ligne de facture réelle
	SourceOnPrem   = "onprem"   // modèle de coût on-prem
)

// CostLine est une ligne de coût journalière, traçable jusqu'à sa source.
type CostLine struct {
	OrgID            string          `json:"org_id"`
	Day              time.Time       `json:"day"`
	ResourceID       string          `json:"resource_id"`
	ConnectorID      string          `json:"connector_id"`
	Provider         string          `json:"provider"`
	ResourceType     string          `json:"resource_type"`
	Region           string          `json:"region"`
	CostType         string          `json:"cost_type"`
	SKU              string          `json:"sku"`
	Quantity         decimal.Decimal `json:"quantity"`
	Unit             string          `json:"unit"`
	Amount           decimal.Decimal `json:"amount"`
	Currency         string          `json:"currency"`
	Source           string          `json:"source"`
	CatalogVersion   string          `json:"catalog_version"`
	AllocationNodeID string          `json:"allocation_node_id"`
	// Détail de traçabilité : ligne de facture, règle de répartition, etc.
	SourceRef string            `json:"source_ref,omitempty"`
	Labels    map[string]string `json:"labels,omitempty"`
}

// BillingLine est une ligne de facture brute importée d'un fournisseur.
type BillingLine struct {
	OrgID       string          `json:"org_id"`
	ConnectorID string          `json:"connector_id"`
	Provider    string          `json:"provider"`
	Day         time.Time       `json:"day"`
	ResourceID  string          `json:"resource_id"`
	Service     string          `json:"service"`
	SKU         string          `json:"sku"`
	CostType    string          `json:"cost_type"`
	Quantity    decimal.Decimal `json:"quantity"`
	Unit        string          `json:"unit"`
	Amount      decimal.Decimal `json:"amount"`
	Currency    string          `json:"currency"`
	InvoiceID   string          `json:"invoice_id"`
}

// Reconciliation compare l'estimé et le facturé pour un fournisseur et un mois.
type Reconciliation struct {
	OrgID       string          `json:"org_id" db:"org_id"`
	ConnectorID string          `json:"connector_id" db:"connector_id"`
	Provider    string          `json:"provider" db:"provider"`
	Month       time.Time       `json:"month" db:"month"`
	Estimated   decimal.Decimal `json:"estimated" db:"estimated"`
	Billed      decimal.Decimal `json:"billed" db:"billed"`
	Currency    string          `json:"currency" db:"currency"`
	ComputedAt  time.Time       `json:"computed_at" db:"computed_at"`
}

// DeltaPercent renvoie l'écart relatif (estimé - facturé) / facturé en pourcentage.
func (r Reconciliation) DeltaPercent() decimal.Decimal {
	if r.Billed.IsZero() {
		return decimal.Zero
	}
	return r.Estimated.Sub(r.Billed).Div(r.Billed).Mul(decimal.NewFromInt(100)).Round(2)
}
