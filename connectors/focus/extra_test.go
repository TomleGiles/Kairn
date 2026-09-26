package focus

import (
	"testing"

	"github.com/kairn-io/kairn/pkg/model"
)

func TestMappings(t *testing.T) {
	for in, want := range map[[2]string]string{
		{"Compute", "Usage"}: model.CostCompute, {"Storage", "Usage"}: model.CostStorage, {"Networking", "Usage"}: model.CostNetwork,
		{"Compute", "Credit"}: model.CostCredit, {"", "Tax"}: model.CostTax, {"Compute", "Purchase"}: model.CostCommitment, {"Other", "Usage"}: model.CostOther,
	} {
		if got := costType(in[0], in[1]); got != want {
			t.Fatalf("costType(%v) = %s", in, got)
		}
	}
	for in, want := range map[[2]string]string{
		{"Databases", ""}: model.TypeDatabase, {"Networking", ""}: model.TypeLoadBalancer, {"Storage", "Azure Blob Storage"}: model.TypeBucket,
		{"Storage", "Amazon EBS"}: model.TypeVolume, {"Analytics", ""}: model.TypeService,
	} {
		if got := resourceType(in[0], in[1]); got != want {
			t.Fatalf("resourceType(%v) = %s", in, got)
		}
	}
	for in, want := range map[string]string{"2026-09-01": "2026-09-01T00:00:00Z", "2026-09-01 10:00:00": "2026-09-01T10:00:00Z", "2026-09-01T10:00:00+02:00": "2026-09-01T10:00:00+02:00"} {
		if got := normTime(in); got != want {
			t.Fatalf("normTime(%s) = %s", in, got)
		}
	}
}
