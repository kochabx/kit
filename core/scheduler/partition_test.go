package scheduler

import (
	"errors"
	"fmt"
	"strings"
	"testing"
)

func TestPartitionNamespaceRouting(t *testing.T) {
	seen := map[string]bool{}
	for i := range 100 {
		key := fmt.Sprint(i)
		namespace, err := PartitionNamespace("jobs", key, 4)
		if err != nil {
			t.Fatal(err)
		}
		again, err := PartitionNamespace("jobs", key, 4)
		if err != nil || namespace != again || !validNamespace.MatchString(namespace) {
			t.Fatal("unstable partition")
		}
		seen[namespace] = true
	}
	if len(seen) != 4 {
		t.Fatalf("partitions=%v", seen)
	}
	for _, tc := range []struct {
		namespace, key string
		count          int
	}{{"invalid space", "x", 4}, {"jobs", "", 4}, {"jobs", "x", 0}, {strings.Repeat("x", 128), "x", 4}} {
		if _, err := PartitionNamespace(tc.namespace, tc.key, tc.count); !errors.Is(err, ErrInvalidArgument) {
			t.Fatalf("invalid route accepted: %+v", tc)
		}
	}
}
