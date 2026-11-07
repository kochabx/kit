package model

import "time"

type ExecutionPolicy struct {
	Timeout       time.Duration `json:"timeout"`
	Retry         RetrySpec     `json:"retry"`
	MaxRecoveries int           `json:"max_recoveries"`
	MaxExecutions int           `json:"max_executions"`
}

type RetrySpec struct {
	Kind        string      `json:"kind"`
	Exponential Exponential `json:"exponential"`
	Identity    string      `json:"identity,omitempty"`
}
