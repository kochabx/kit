package model

import "time"

type Stats struct {
	Scheduled, Ready, Pending, Running, Dead, Schedules int64
	ObserverDropped                                     uint64
	DispatchLag                                         time.Duration
	PendingHeadIdle                                     time.Duration
}
