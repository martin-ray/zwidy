package ipam

import (
	"path/filepath"
	"testing"
)

func TestAllocateStableIP(t *testing.T) {
	path := filepath.Join(t.TempDir(), "zwidy.db")
	store, err := Open(path, "10.77.0.0/29", "10.77.0.1", map[string]string{})
	if err != nil {
		t.Fatal(err)
	}
	ip1, err := store.Allocate("node-a")
	if err != nil {
		t.Fatal(err)
	}
	ip2, err := store.Allocate("node-a")
	if err != nil {
		t.Fatal(err)
	}
	if ip1 != ip2 {
		t.Fatalf("expected stable IP, got %s and %s", ip1, ip2)
	}
}
