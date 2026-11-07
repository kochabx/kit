package scheduler

import (
	"github.com/kochabx/kit/core/scheduler/internal/model"
)

type CronSpec = model.CronSpec

type ScheduleState = model.ScheduleState

type Schedule = model.Schedule

const (
	ScheduleActive    = model.ScheduleActive
	SchedulePaused    = model.SchedulePaused
	ScheduleCancelled = model.ScheduleCancelled
	ScheduleInvalid   = model.ScheduleInvalid
)

type ScheduleQuery = model.ScheduleQuery
