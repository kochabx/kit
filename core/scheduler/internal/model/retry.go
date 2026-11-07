package model

import (
	"math"
	"math/rand/v2"
	"time"
)

type Exponential struct {
	MaxAttempts int
	Initial     time.Duration
	Max         time.Duration
	Multiplier  float64
	Jitter      bool
}

func (p Exponential) NextDelay(attempt int, _ error) (time.Duration, bool) {
	if p.MaxAttempts <= 0 || attempt >= p.MaxAttempts {
		return 0, false
	}
	initial := p.Initial
	if initial <= 0 {
		initial = time.Second
	}
	maximum := p.Max
	if maximum <= 0 {
		maximum = time.Hour
	}
	multiplier := p.Multiplier
	if multiplier < 1 || math.IsNaN(multiplier) {
		multiplier = 2
	}
	d := math.Min(float64(maximum), float64(initial)*math.Pow(multiplier, float64(max(1, attempt)-1)))
	if p.Jitter {
		d *= 0.5 + rand.Float64()*0.5
	}
	// float64(MaxInt64) rounds to 2^63, outside time.Duration.
	// Return the integer cap before converting any rounded boundary value.
	if d >= float64(maximum) {
		return maximum, true
	}
	return time.Duration(d), true
}
