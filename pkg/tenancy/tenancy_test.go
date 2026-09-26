package tenancy

import (
	"context"
	"errors"
	"testing"
)

func TestOrgIDMissing(t *testing.T) {
	if _, err := OrgID(context.Background()); !errors.Is(err, ErrNoOrg) {
		t.Fatalf("want ErrNoOrg, got %v", err)
	}
	if _, err := OrgID(WithOrg(context.Background(), "")); !errors.Is(err, ErrNoOrg) {
		t.Fatalf("empty org must be rejected, got %v", err)
	}
}

func TestCheck(t *testing.T) {
	ctx := WithOrg(context.Background(), "org-a")
	if err := Check(ctx, "org-a"); err != nil {
		t.Fatalf("same org: %v", err)
	}
	if err := Check(ctx, "org-b"); !errors.Is(err, ErrCrossOrg) {
		t.Fatalf("want ErrCrossOrg, got %v", err)
	}
	if err := Check(context.Background(), "org-a"); !errors.Is(err, ErrNoOrg) {
		t.Fatalf("want ErrNoOrg, got %v", err)
	}
}

func TestSystem(t *testing.T) {
	if IsSystem(context.Background()) {
		t.Fatal("background is not system")
	}
	if !IsSystem(WithSystem(context.Background())) {
		t.Fatal("WithSystem must mark context")
	}
}
