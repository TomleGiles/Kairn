package provider

import (
	"context"
	"fmt"
	"slices"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
)

// oneOfValidator restreint une chaîne à une liste de valeurs.
type oneOfValidator struct{ values []string }

func oneOf(values ...string) validator.String { return oneOfValidator{values: values} }

func (v oneOfValidator) Description(context.Context) string {
	return "one of: " + strings.Join(v.values, ", ")
}

func (v oneOfValidator) MarkdownDescription(ctx context.Context) string { return v.Description(ctx) }

func (v oneOfValidator) ValidateString(_ context.Context, req validator.StringRequest, resp *validator.StringResponse) {
	if req.ConfigValue.IsNull() || req.ConfigValue.IsUnknown() {
		return
	}
	if !slices.Contains(v.values, req.ConfigValue.ValueString()) {
		resp.Diagnostics.AddAttributeError(req.Path, "Invalid value",
			fmt.Sprintf("%q is not allowed; expected %s", req.ConfigValue.ValueString(), strings.Join(v.values, ", ")))
	}
}
