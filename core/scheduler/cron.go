package scheduler

import (
	"fmt"
	"strings"
	"time"

	"github.com/robfig/cron/v3"
)

// parseCron makes the explicit time zone authoritative across machines.
func parseCron(spec CronSpec) (cron.Schedule, *time.Location, error) {
	zone := spec.TimeZone
	if zone == "" {
		zone = "UTC"
	}
	if zone == "Local" {
		return nil, nil, fmt.Errorf("use UTC or an IANA time zone: %w", ErrInvalidArgument)
	}
	expression := strings.TrimSpace(spec.Expression)
	if strings.HasPrefix(expression, "CRON_TZ=") || strings.HasPrefix(expression, "TZ=") {
		return nil, nil, fmt.Errorf("set CronSpec.TimeZone explicitly: %w", ErrInvalidArgument)
	}
	location, err := time.LoadLocation(zone)
	if err != nil {
		return nil, nil, fmt.Errorf("load cron time zone: %w: %w", ErrInvalidArgument, err)
	}
	parsed, err := cron.ParseStandard(expression)
	if err != nil {
		return nil, nil, fmt.Errorf("parse cron: %w: %w", ErrInvalidArgument, err)
	}
	return parsed, location, nil
}

func nextRunMillis(schedule cron.Schedule, location *time.Location, from int64) (int64, error) {
	next := schedule.Next(time.UnixMilli(from).In(location))
	if next.IsZero() || next.UnixMilli() <= from {
		return 0, fmt.Errorf("cron has no future occurrence: %w", ErrInvalidArgument)
	}
	return next.UnixMilli(), nil
}
