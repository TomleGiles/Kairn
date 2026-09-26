package api

import (
	"encoding/json"

	"github.com/kairn-io/kairn/pkg/ids"
)

func newID() string { return ids.New() }

func jsonMarshal(v any) ([]byte, error) { return json.Marshal(v) }
