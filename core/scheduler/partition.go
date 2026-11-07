package scheduler

import (
	"crypto/sha256"
	"encoding/binary"
	"fmt"
)

// PartitionNamespace deterministically routes a business key to an independent
// Redis hash tag. Run one engine per partition and route all operations for
// the same business/unique key to it. Changing count requires draining or migrating
// existing partitions; jobs and schedules are never migrated automatically.
func PartitionNamespace(namespace, key string, count int) (string, error) {
	if !validNamespace.MatchString(namespace) || key == "" || count < 1 {
		return "", ErrInvalidArgument
	}
	digest := sha256.Sum256([]byte(key))
	partition := binary.BigEndian.Uint64(digest[:8]) % uint64(count)
	result := fmt.Sprintf("%s.p%d", namespace, partition)
	if !validNamespace.MatchString(result) {
		return "", ErrInvalidArgument
	}
	return result, nil
}
