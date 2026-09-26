package pgstore

import (
	"encoding/json"
	"fmt"
	"reflect"
	"strconv"
	"strings"
	"time"

	"github.com/shopspring/decimal"
)

// colInfo décrit une colonne (information_schema.columns).
type colInfo struct {
	name     string
	dataType string // uuid, jsonb, numeric, ARRAY, timestamp with time zone, date, text, integer, boolean, double precision, bytea
	udt      string // _text, _int4…
	nullable bool
}

// Les uuid, numeric et jsonb transitent en texte (conversions explicites
// côté SQL) : aucune dépendance aux codecs pgx, montants sans float.
func (c colInfo) textCast() bool {
	return c.dataType == "uuid" || c.dataType == "numeric" || c.dataType == "jsonb"
}

func (c colInfo) timeLike() bool {
	return strings.HasPrefix(c.dataType, "timestamp") || c.dataType == "date"
}

type fieldMap struct {
	col   colInfo
	index []int
	typ   reflect.Type
	json  string
}

// tableMap relie les champs `db` d'une struct aux colonnes d'une table.
type tableMap struct {
	table  string
	fields []fieldMap
	byCol  map[string]int
	byJSON map[string]string
}

var (
	timeType    = reflect.TypeOf(time.Time{})
	decimalType = reflect.TypeOf(decimal.Decimal{})
)

func buildMap(table string, t reflect.Type, cols map[string]colInfo) *tableMap {
	m := &tableMap{table: table, byCol: map[string]int{}, byJSON: map[string]string{}}
	for i := 0; i < t.NumField(); i++ {
		f := t.Field(i)
		name := strings.Split(f.Tag.Get("db"), ",")[0]
		if name == "" || name == "-" {
			continue
		}
		c, ok := cols[name]
		if !ok {
			panic(fmt.Sprintf("pgstore: %s.%s: column not found (migration missing?)", table, name))
		}
		m.byCol[name] = len(m.fields)
		js := strings.Split(f.Tag.Get("json"), ",")[0]
		if js != "" && js != "-" {
			m.byJSON[js] = name
		}
		m.fields = append(m.fields, fieldMap{col: c, index: f.Index, typ: f.Type, json: js})
	}
	return m
}

// selectList renvoie la liste des colonnes à lire, avec conversions en texte.
func (m *tableMap) selectList(alias string) string {
	parts := make([]string, len(m.fields))
	prefix := ""
	if alias != "" {
		prefix = alias + "."
	}
	for i, f := range m.fields {
		if f.col.textCast() {
			// Alias distinct : un ORDER BY sur le nom de colonne doit trier la valeur typée, pas son texte.
			parts[i] = prefix + f.col.name + "::text AS " + f.col.name + "__txt"
		} else {
			parts[i] = prefix + f.col.name
		}
	}
	return strings.Join(parts, ", ")
}

// placeholder renvoie le paramètre $n avec la conversion SQL adaptée à la colonne.
func (f fieldMap) placeholder(n int) string {
	p := "$" + strconv.Itoa(n)
	switch f.col.dataType {
	case "uuid":
		return p + "::uuid"
	case "numeric":
		return p + "::numeric"
	case "jsonb":
		return p + "::jsonb"
	}
	return p
}

// encode convertit la valeur d'un champ en paramètre SQL.
func (f fieldMap) encode(v reflect.Value) (any, error) {
	c := f.col
	switch {
	case c.dataType == "jsonb":
		if (v.Kind() == reflect.Pointer || v.Kind() == reflect.Map || v.Kind() == reflect.Slice) && v.IsNil() {
			if c.nullable && v.Kind() == reflect.Pointer {
				return nil, nil
			}
			if v.Kind() == reflect.Slice {
				return "[]", nil
			}
			return "{}", nil
		}
		b, err := json.Marshal(v.Interface())
		if err != nil {
			return nil, fmt.Errorf("pgstore: encode %s: %w", c.name, err)
		}
		return string(b), nil
	case c.dataType == "numeric":
		switch v.Type() {
		case decimalType:
			return v.Interface().(decimal.Decimal).String(), nil
		case reflect.PointerTo(decimalType):
			if v.IsNil() {
				return nil, nil
			}
			return v.Elem().Interface().(decimal.Decimal).String(), nil
		}
	case c.dataType == "uuid":
		s := ""
		if v.Kind() == reflect.Pointer {
			if v.IsNil() {
				return nil, nil
			}
			s = v.Elem().String()
		} else {
			s = v.String()
		}
		if s == "" {
			return nil, nil
		}
		return s, nil
	case c.timeLike():
		if v.Kind() == reflect.Pointer {
			if v.IsNil() {
				return nil, nil
			}
			return v.Elem().Interface().(time.Time).UTC(), nil
		}
		t := v.Interface().(time.Time)
		if t.IsZero() && c.nullable {
			return nil, nil
		}
		return t.UTC(), nil
	case c.dataType == "ARRAY":
		if c.udt == "_int4" || c.udt == "_int8" {
			out := make([]int64, v.Len())
			for i := range out {
				out[i] = v.Index(i).Int()
			}
			return out, nil
		}
		out := make([]string, v.Len())
		for i := range out {
			out[i] = v.Index(i).String()
		}
		return out, nil
	case c.dataType == "bytea":
		if v.IsNil() {
			return nil, nil
		}
		return v.Bytes(), nil
	}
	switch v.Kind() {
	case reflect.Pointer:
		if v.IsNil() {
			return nil, nil
		}
		return f.encodeScalar(v.Elem()), nil
	default:
		return f.encodeScalar(v), nil
	}
}

func (f fieldMap) encodeScalar(v reflect.Value) any {
	switch v.Kind() {
	case reflect.String:
		return v.String()
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return v.Int()
	case reflect.Bool:
		return v.Bool()
	case reflect.Float32, reflect.Float64:
		return v.Float()
	}
	return v.Interface()
}

// dest renvoie la cible de lecture d'une colonne et la fonction d'affectation au champ.
func (f fieldMap) dest() (any, func(reflect.Value) error) {
	c := f.col
	switch {
	case c.textCast():
		var p *string
		return &p, func(v reflect.Value) error { return f.assignText(v, p) }
	case c.timeLike():
		var p *time.Time
		return &p, func(v reflect.Value) error {
			if p == nil {
				return nil
			}
			t := p.UTC()
			if v.Kind() == reflect.Pointer {
				v.Set(reflect.ValueOf(&t))
			} else {
				v.Set(reflect.ValueOf(t))
			}
			return nil
		}
	case c.dataType == "ARRAY":
		if c.udt == "_int4" || c.udt == "_int8" {
			var p []int64
			return &p, func(v reflect.Value) error {
				out := reflect.MakeSlice(v.Type(), len(p), len(p))
				for i, x := range p {
					out.Index(i).SetInt(x)
				}
				v.Set(out)
				return nil
			}
		}
		var p []string
		return &p, func(v reflect.Value) error {
			out := reflect.MakeSlice(v.Type(), len(p), len(p))
			for i, x := range p {
				out.Index(i).SetString(x)
			}
			v.Set(out)
			return nil
		}
	case c.dataType == "bytea":
		var p []byte
		return &p, func(v reflect.Value) error {
			if p != nil {
				v.SetBytes(p)
			}
			return nil
		}
	case c.dataType == "boolean":
		var p *bool
		return &p, func(v reflect.Value) error {
			if p != nil {
				v.SetBool(*p)
			}
			return nil
		}
	case c.dataType == "integer" || c.dataType == "bigint" || c.dataType == "smallint":
		var p *int64
		return &p, func(v reflect.Value) error {
			if p != nil {
				v.SetInt(*p)
			}
			return nil
		}
	case c.dataType == "double precision" || c.dataType == "real":
		var p *float64
		return &p, func(v reflect.Value) error {
			if p != nil {
				v.SetFloat(*p)
			}
			return nil
		}
	}
	var p *string
	return &p, func(v reflect.Value) error {
		if p == nil {
			return nil
		}
		if v.Kind() == reflect.Pointer {
			nv := reflect.New(v.Type().Elem())
			nv.Elem().SetString(*p)
			v.Set(nv)
			return nil
		}
		v.SetString(*p)
		return nil
	}
}

func (f fieldMap) assignText(v reflect.Value, p *string) error {
	if p == nil {
		return nil
	}
	s := *p
	switch {
	case f.col.dataType == "jsonb":
		// UseNumber : les nombres des attributs libres restent exacts (json.Number).
		dec := json.NewDecoder(strings.NewReader(s))
		dec.UseNumber()
		if err := dec.Decode(v.Addr().Interface()); err != nil {
			return fmt.Errorf("pgstore: decode %s: %w", f.col.name, err)
		}
		return nil
	case v.Type() == decimalType || v.Type() == reflect.PointerTo(decimalType):
		d, err := decimal.NewFromString(s)
		if err != nil {
			return fmt.Errorf("pgstore: decode %s: %w", f.col.name, err)
		}
		if v.Kind() == reflect.Pointer {
			v.Set(reflect.ValueOf(&d))
		} else {
			v.Set(reflect.ValueOf(d))
		}
		return nil
	case v.Kind() == reflect.Pointer:
		nv := reflect.New(v.Type().Elem())
		nv.Elem().SetString(s)
		v.Set(nv)
		return nil
	default:
		v.SetString(s)
		return nil
	}
}

// scanner prépare la lecture d'une ligne dans une valeur de type T.
type scanner struct {
	dests   []any
	assigns []func(reflect.Value) error
	m       *tableMap
}

func (m *tableMap) scanner() *scanner {
	sc := &scanner{m: m, dests: make([]any, len(m.fields)), assigns: make([]func(reflect.Value) error, len(m.fields))}
	for i, f := range m.fields {
		sc.dests[i], sc.assigns[i] = f.dest()
	}
	return sc
}

func (sc *scanner) into(target reflect.Value) error {
	for i, f := range sc.m.fields {
		if err := sc.assigns[i](target.FieldByIndex(f.index)); err != nil {
			return err
		}
	}
	return nil
}

// values encode les champs d'une struct, dans l'ordre des colonnes.
func (m *tableMap) values(v reflect.Value, skip map[string]bool) (cols []string, holders []string, args []any, err error) {
	for _, f := range m.fields {
		if skip[f.col.name] {
			continue
		}
		a, err := f.encode(v.FieldByIndex(f.index))
		if err != nil {
			return nil, nil, nil, err
		}
		args = append(args, a)
		cols = append(cols, f.col.name)
		holders = append(holders, f.placeholder(len(args)))
	}
	return cols, holders, args, nil
}
