package ids

import "testing"

func TestResourceDeterministic(t *testing.T) {
	a := Resource("org", "conn", "openstack.server", "abc")
	b := Resource("org", "conn", "openstack.server", "abc")
	if a != b {
		t.Fatalf("resource ids must be deterministic: %s != %s", a, b)
	}
	if Resource("org2", "conn", "openstack.server", "abc") == a {
		t.Fatal("different org must yield different id")
	}
	if !Valid(a) {
		t.Fatalf("invalid uuid %s", a)
	}
}

func TestNewOrdered(t *testing.T) {
	prev := New()
	for i := 0; i < 100; i++ {
		n := New()
		if n <= prev {
			t.Fatalf("UUIDv7 must be monotonic as strings: %s <= %s", n, prev)
		}
		prev = n
	}
}
