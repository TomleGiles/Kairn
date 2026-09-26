package main

import (
	"encoding/csv"
	"encoding/json"
	"fmt"
	"strings"
	"text/tabwriter"
	"unicode/utf8"
)

// table est une sortie tabulaire rendue en texte aligné, JSON ou CSV.
type table struct {
	head   []string
	rows   [][]string
	footer string
}

func (t *table) add(cells ...string) { t.rows = append(t.rows, cells) }

func (t *table) print(format string) error {
	switch format {
	case "json":
		objs := make([]map[string]string, 0, len(t.rows))
		for _, r := range t.rows {
			o := map[string]string{}
			for i, h := range t.head {
				if i < len(r) && h != "" {
					o[strings.ToLower(h)] = r[i]
				}
			}
			objs = append(objs, o)
		}
		return printJSON(objs)
	case "csv":
		w := csv.NewWriter(out)
		if err := w.Write(t.head); err != nil {
			return err
		}
		if err := w.WriteAll(t.rows); err != nil {
			return err
		}
		w.Flush()
		return w.Error()
	case "table", "":
		tw := tabwriter.NewWriter(out, 0, 0, 2, ' ', 0)
		fmt.Fprintln(tw, strings.Join(t.head, "\t"))
		for _, r := range t.rows {
			cells := make([]string, len(r))
			for i, c := range r {
				cells[i] = clip(c, 60)
			}
			fmt.Fprintln(tw, strings.Join(cells, "\t"))
		}
		if err := tw.Flush(); err != nil {
			return err
		}
		if t.footer != "" {
			fmt.Fprintln(out, t.footer)
		}
		if len(t.rows) == 0 {
			fmt.Fprintln(out, "(aucun résultat)")
		}
		return nil
	}
	return fmt.Errorf("unknown output format %q (table, json, csv)", format)
}

func clip(s string, n int) string {
	if utf8.RuneCountInString(s) <= n {
		return s
	}
	r := []rune(s)
	return string(r[:n-1]) + "…"
}

func printJSON(v any) error {
	enc := json.NewEncoder(out)
	enc.SetIndent("", "  ")
	return enc.Encode(v)
}
