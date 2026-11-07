package store

import (
	"strings"
	"testing"
)

func TestQueueIndexesAndNamespaceEntitiesHaveDistinctScopes(t *testing.T) {
	namespace := NewNamespaceKeys("production")
	a, b := namespace.Queue("mail"), namespace.Queue("reports")
	if a.Job("id") != b.Job("id") || a.Schedule("id") != b.Schedule("id") || a.Unique("business") != b.Unique("business") {
		t.Fatal("namespace entity identity changed with queue")
	}
	if a.Ready() == b.Ready() || a.Scheduled() == b.Scheduled() || a.Dead() == b.Dead() || a.MaintenanceLease() == b.MaintenanceLease() {
		t.Fatal("queue indexes or leases overlap")
	}
	for _, key := range []string{a.Job("id"), a.Schedule("id"), a.Unique("business"), a.Ready(), b.Ready(), a.Scheduled(), b.Dead()} {
		if !strings.Contains(key, "{production}:v1:") {
			t.Fatalf("key left namespace slot/version: %s", key)
		}
	}
}
