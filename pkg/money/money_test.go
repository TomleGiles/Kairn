package money

import (
	"encoding/json"
	"errors"
	"testing"
)

func TestAddSameCurrency(t *testing.T) {
	a := MustParse("0.1", EUR)
	b := MustParse("0.2", EUR)
	got, err := a.Add(b)
	if err != nil {
		t.Fatal(err)
	}
	if !got.Value.Equal(Dec("0.3")) {
		t.Fatalf("0.1+0.2 = %s, want 0.3 (no float drift)", got.Value)
	}
}

func TestAddMismatch(t *testing.T) {
	_, err := MustParse("1", EUR).Add(MustParse("1", USD))
	if !errors.Is(err, ErrCurrencyMismatch) {
		t.Fatalf("want ErrCurrencyMismatch, got %v", err)
	}
}

func TestJSONIsString(t *testing.T) {
	b, err := json.Marshal(MustParse("12.345678", EUR))
	if err != nil {
		t.Fatal(err)
	}
	if string(b) != `{"value":"12.345678","currency":"EUR"}` {
		t.Fatalf("unexpected json %s", b)
	}
	var back Amount
	if err := json.Unmarshal(b, &back); err != nil {
		t.Fatal(err)
	}
	if !back.Value.Equal(Dec("12.345678")) || back.Currency != EUR {
		t.Fatalf("round trip failed: %+v", back)
	}
}

func TestRoundAndString(t *testing.T) {
	a := MustParse("10.005", EUR)
	if a.String() != "10.01 EUR" {
		t.Fatalf("got %s", a.String())
	}
	if !a.Round(2).Value.Equal(Dec("10.01")) {
		t.Fatalf("round half away from zero expected")
	}
}

func TestSum(t *testing.T) {
	s, err := Sum(EUR, MustParse("1.10", EUR), MustParse("2.20", EUR), MustParse("3.30", EUR))
	if err != nil {
		t.Fatal(err)
	}
	if !s.Value.Equal(Dec("6.6")) {
		t.Fatalf("got %s", s.Value)
	}
	empty, _ := Sum(USD)
	if !empty.IsZero() || empty.Currency != USD {
		t.Fatalf("empty sum should be 0 USD")
	}
}
