// Package export sérialise les lignes de coût (CSV, Parquet) pour les
// exports ponctuels et planifiés (M-12). Les montants restent des chaînes décimales.
package export

import (
	"bytes"
	"encoding/csv"
	"fmt"

	"github.com/parquet-go/parquet-go"

	"github.com/kairn-io/kairn/pkg/model"
)

// costRecord est le schéma d'export Parquet (montants en chaînes décimales).
type costRecord struct {
	Day              string `parquet:"day"`
	ResourceID       string `parquet:"resource_id"`
	ConnectorID      string `parquet:"connector_id"`
	Provider         string `parquet:"provider"`
	ResourceType     string `parquet:"resource_type"`
	Region           string `parquet:"region"`
	CostType         string `parquet:"cost_type"`
	SKU              string `parquet:"sku"`
	Quantity         string `parquet:"quantity"`
	Unit             string `parquet:"unit"`
	Amount           string `parquet:"amount"`
	Currency         string `parquet:"currency"`
	Source           string `parquet:"source"`
	CatalogVersion   string `parquet:"catalog_version"`
	AllocationNodeID string `parquet:"allocation_node_id"`
	SourceRef        string `parquet:"source_ref"`
}

// EncodeCostLines sérialise des lignes de coût en CSV ou Parquet.
func EncodeCostLines(lines []model.CostLine, format string) ([]byte, string, error) {
	recs := make([]costRecord, 0, len(lines))
	for _, l := range lines {
		recs = append(recs, costRecord{
			Day: l.Day.Format("2006-01-02"), ResourceID: l.ResourceID, ConnectorID: l.ConnectorID, Provider: l.Provider,
			ResourceType: l.ResourceType, Region: l.Region, CostType: l.CostType, SKU: l.SKU, Quantity: l.Quantity.String(),
			Unit: l.Unit, Amount: l.Amount.StringFixed(6), Currency: l.Currency, Source: l.Source, CatalogVersion: l.CatalogVersion,
			AllocationNodeID: l.AllocationNodeID, SourceRef: l.SourceRef,
		})
	}
	var buf bytes.Buffer
	if format == "parquet" {
		if err := parquet.Write(&buf, recs); err != nil {
			return nil, "", fmt.Errorf("parquet: %w", err)
		}
		return buf.Bytes(), "application/vnd.apache.parquet", nil
	}
	w := csv.NewWriter(&buf)
	_ = w.Write([]string{"day", "resource_id", "connector_id", "provider", "resource_type", "region", "cost_type", "sku", "quantity", "unit", "amount", "currency", "source", "catalog_version", "allocation_node_id", "source_ref"})
	for _, r := range recs {
		_ = w.Write([]string{r.Day, r.ResourceID, r.ConnectorID, r.Provider, r.ResourceType, r.Region, r.CostType, r.SKU, r.Quantity, r.Unit, r.Amount, r.Currency, r.Source, r.CatalogVersion, r.AllocationNodeID, r.SourceRef})
	}
	w.Flush()
	return buf.Bytes(), "text/csv; charset=utf-8", w.Error()
}
