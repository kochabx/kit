// Package store owns Redis persistence, key layout, and atomic transitions.
package store

import (
	"context"
	"embed"

	"github.com/redis/go-redis/v9"
)

//go:embed lua/*.lua
var scripts embed.FS

// Script uses Redis' script cache for individual calls. Eval submits source in a
// pipeline, where automatic NOSCRIPT recovery cannot safely replay other calls.
type Script struct {
	*redis.Script
	source string
}

func (s Script) Eval(ctx context.Context, pipe redis.Pipeliner, keys []string, args ...any) *redis.Cmd {
	return pipe.Eval(ctx, s.source, keys, args...)
}

func load(name string) Script {
	source, err := scripts.ReadFile("lua/" + name + ".lua")
	if err != nil {
		panic(err)
	}
	if name == "claim" || name == "complete" || name == "fail" {
		helper, err := scripts.ReadFile("lua/attempt_history.lua")
		if err != nil {
			panic(err)
		}
		source = append(append(helper, '\n'), source...)
		if name == "complete" || name == "fail" {
			common, err := scripts.ReadFile("lua/finish_helpers.lua")
			if err != nil {
				panic(err)
			}
			source = append(append(common, '\n'), source...)
		}
	}
	return Script{Script: redis.NewScript(string(source)), source: string(source)}
}

var (
	ReleaseDelivery       = load("release_delivery")
	Time                  = load("time")
	DeleteConsumer        = load("delete_consumer")
	Enqueue               = load("enqueue")
	Dispatch              = load("dispatch")
	Claim                 = load("claim")
	Complete              = load("complete")
	Fail                  = load("fail")
	Cancel                = load("cancel")
	Discard               = load("discard")
	CreateSchedule        = load("create_schedule")
	FireSchedule          = load("fire_schedule")
	MaintenanceRenew      = load("maintenance_renew")
	MaintenanceRelease    = load("maintenance_release")
	PruneExpiredDeadIndex = load("prune_expired_dead_index")
	PurgeExpiredSchedules = load("purge_expired_schedules")
	DueSchedules          = load("due_schedules")
	RetryDead             = load("retry_dead")
	DeleteJob             = load("delete_job")
	StatsScript           = load("stats")
	DisableSchedule       = load("disable_schedule")
	CancelSchedule        = load("cancel_schedule")
	SetScheduleState      = load("set_schedule_state")
	DeleteSchedule        = load("delete_schedule")
	UpdateSchedule        = load("update_schedule")
	RenewLease            = load("renew_lease")
	CheckCancellation     = load("check_cancellation")
)
