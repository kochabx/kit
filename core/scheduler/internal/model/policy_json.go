package model

import (
	"encoding/json/jsontext"
	"encoding/json/v2"
	"time"
)

// The Redis execution protocol stores durations as integer nanoseconds.
// Explicit wire fields avoid relying on a default time.Duration representation.
type policyJSON struct {
	Timeout       int64     `json:"timeout"`
	Retry         RetrySpec `json:"retry"`
	MaxRecoveries int       `json:"max_recoveries"`
	MaxExecutions int       `json:"max_executions"`
}

func (p ExecutionPolicy) MarshalJSONTo(enc *jsontext.Encoder) error {
	return json.MarshalEncode(enc, policyJSON{int64(p.Timeout), p.Retry, p.MaxRecoveries, p.MaxExecutions})
}

func (p *ExecutionPolicy) UnmarshalJSONFrom(dec *jsontext.Decoder) error {
	wire := policyJSON{int64(p.Timeout), p.Retry, p.MaxRecoveries, p.MaxExecutions}
	if err := json.UnmarshalDecode(dec, &wire); err != nil {
		return err
	}
	*p = ExecutionPolicy{time.Duration(wire.Timeout), wire.Retry, wire.MaxRecoveries, wire.MaxExecutions}
	return nil
}

type exponentialJSON struct {
	MaxAttempts int
	Initial     int64
	Max         int64
	Multiplier  float64
	Jitter      bool
}

func (p Exponential) MarshalJSONTo(enc *jsontext.Encoder) error {
	return json.MarshalEncode(enc, exponentialJSON{p.MaxAttempts, int64(p.Initial), int64(p.Max), p.Multiplier, p.Jitter})
}

func (p *Exponential) UnmarshalJSONFrom(dec *jsontext.Decoder) error {
	wire := exponentialJSON{p.MaxAttempts, int64(p.Initial), int64(p.Max), p.Multiplier, p.Jitter}
	if err := json.UnmarshalDecode(dec, &wire); err != nil {
		return err
	}
	*p = Exponential{wire.MaxAttempts, time.Duration(wire.Initial), time.Duration(wire.Max), wire.Multiplier, wire.Jitter}
	return nil
}
