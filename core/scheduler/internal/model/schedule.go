package model

import "time"

type CronSpec struct {
	Expression string
	TimeZone   string
}

type ScheduleState string

type Schedule struct {
	Queue                              string
	ID, TaskName, Expression, TimeZone string
	NextAt                             time.Time
	State                              ScheduleState
	LastRunAt                          time.Time
}

type ScheduleQuery struct {
	Cursor uint64
	Count  int64
	State  ScheduleState
}

const (
	ScheduleActive    ScheduleState = "active"
	SchedulePaused    ScheduleState = "paused"
	ScheduleCancelled ScheduleState = "cancelled"
	ScheduleInvalid   ScheduleState = "invalid"
)
