// Package money fournit un type monétaire décimal. Aucun montant ne doit
// transiter par un float : les calculs utilisent shopspring/decimal et les
// montants sont toujours accompagnés de leur devise.
package money

import (
	"errors"
	"fmt"
	"strings"

	"github.com/shopspring/decimal"
)

// Currency est un code ISO 4217.
type Currency string

// Devises courantes.
const (
	EUR Currency = "EUR"
	USD Currency = "USD"
	GBP Currency = "GBP"
	CHF Currency = "CHF"
)

// StoragePlaces est la précision de stockage (ClickHouse Decimal(18,6)).
const StoragePlaces = 6

// DisplayPlaces est la précision d'affichage (centimes).
const DisplayPlaces = 2

// HoursPerMonth est la convention de facturation des fournisseurs (730 h).
var HoursPerMonth = decimal.NewFromInt(730)

// ErrCurrencyMismatch est renvoyée lors d'une opération entre devises différentes.
var ErrCurrencyMismatch = errors.New("money: currency mismatch")

// Amount est un montant dans une devise donnée. Sérialisé en JSON avec la
// valeur sous forme de chaîne : {"value":"12.340000","currency":"EUR"}.
type Amount struct {
	Value    decimal.Decimal `json:"value"`
	Currency Currency        `json:"currency"`
}

// New construit un montant.
func New(v decimal.Decimal, c Currency) Amount { return Amount{Value: v, Currency: c} }

// Zero renvoie un montant nul dans la devise donnée.
func Zero(c Currency) Amount { return Amount{Value: decimal.Zero, Currency: c} }

// Parse lit un montant décimal depuis une chaîne.
func Parse(s string, c Currency) (Amount, error) {
	d, err := decimal.NewFromString(strings.TrimSpace(s))
	if err != nil {
		return Amount{}, fmt.Errorf("money: parse %q: %w", s, err)
	}
	return Amount{Value: d, Currency: c}, nil
}

// MustParse est Parse qui panique ; réservé aux tests et constantes.
func MustParse(s string, c Currency) Amount {
	a, err := Parse(s, c)
	if err != nil {
		panic(err)
	}
	return a
}

// Add additionne deux montants de même devise.
func (a Amount) Add(b Amount) (Amount, error) {
	if a.Currency != b.Currency {
		return Amount{}, fmt.Errorf("%w: %s + %s", ErrCurrencyMismatch, a.Currency, b.Currency)
	}
	return Amount{Value: a.Value.Add(b.Value), Currency: a.Currency}, nil
}

// Sub soustrait deux montants de même devise.
func (a Amount) Sub(b Amount) (Amount, error) {
	if a.Currency != b.Currency {
		return Amount{}, fmt.Errorf("%w: %s - %s", ErrCurrencyMismatch, a.Currency, b.Currency)
	}
	return Amount{Value: a.Value.Sub(b.Value), Currency: a.Currency}, nil
}

// Mul multiplie un montant par une quantité décimale.
func (a Amount) Mul(q decimal.Decimal) Amount {
	return Amount{Value: a.Value.Mul(q), Currency: a.Currency}
}

// Round arrondit à n décimales (arrondi commercial, demi vers l'extérieur).
func (a Amount) Round(places int32) Amount {
	return Amount{Value: a.Value.Round(places), Currency: a.Currency}
}

// Storage arrondit à la précision de stockage.
func (a Amount) Storage() Amount { return a.Round(StoragePlaces) }

// IsZero indique si le montant est nul.
func (a Amount) IsZero() bool { return a.Value.IsZero() }

// String renvoie une représentation lisible, ex. "12.34 EUR".
func (a Amount) String() string {
	return a.Value.StringFixed(DisplayPlaces) + " " + string(a.Currency)
}

// Sum additionne une liste de montants de même devise. Une liste vide renvoie zéro dans c.
func Sum(c Currency, amounts ...Amount) (Amount, error) {
	total := Zero(c)
	for _, x := range amounts {
		var err error
		if total, err = total.Add(x); err != nil {
			return Amount{}, err
		}
	}
	return total, nil
}

// Dec est un raccourci pour construire un décimal depuis une chaîne constante.
func Dec(s string) decimal.Decimal { return decimal.RequireFromString(s) }
