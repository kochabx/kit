package kafka

import (
	"context"
	"crypto/tls"
	"testing"
	"time"

	"github.com/twmb/franz-go/pkg/kgo"
)

func TestConfigValidate(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name    string
		cfg     Config
		wantErr bool
	}{
		{
			name: "valid defaults",
			cfg:  Config{Brokers: []string{"127.0.0.1:9092"}},
		},
		{
			name: "valid no ack new(0)",
			cfg:  Config{Brokers: []string{"127.0.0.1:9092"}, Acks: new(0)},
		},
		{
			name: "valid scram",
			cfg: Config{
				Brokers: []string{"127.0.0.1:9092"},
				SASL:    &SASLConfig{Mechanism: "SCRAM-SHA-256", Username: "u", Password: "p"},
			},
		},
		{
			name:    "empty brokers",
			cfg:     Config{},
			wantErr: true,
		},
		{
			name:    "invalid acks",
			cfg:     Config{Brokers: []string{"127.0.0.1:9092"}, Acks: new(2)},
			wantErr: true,
		},
		{
			name: "unsupported sasl",
			cfg: Config{
				Brokers: []string{"127.0.0.1:9092"},
				SASL:    &SASLConfig{Mechanism: "GSSAPI", Username: "u", Password: "p"},
			},
			wantErr: true,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			err := tc.cfg.Validate()
			if (err != nil) != tc.wantErr {
				t.Fatalf("Validate() error = %v, wantErr %v", err, tc.wantErr)
			}
		})
	}
}

func TestAcksZeroPreservedAndTLSClone(t *testing.T) {
	t.Parallel()

	// Explicit new(0) must resolve to NoAck and disable idempotent writes (required by franz-go).
	cfg := Config{
		Brokers: []string{"127.0.0.1:9092"},
		Acks:    new(0),
	}
	acks, disableIdempotent := cfg.resolveAcks()
	if acks != kgo.NoAck() || !disableIdempotent {
		t.Fatalf("resolveAcks() = (%v, %v), want (NoAck, true)", acks, disableIdempotent)
	}

	// Default nil Acks must resolve to AllISRAcks.
	defaultAcks, defaultDisable := (Config{Brokers: []string{"127.0.0.1:9092"}}).resolveAcks()
	if defaultAcks != kgo.AllISRAcks() || defaultDisable {
		t.Fatalf("default resolveAcks() = (%v, %v), want (AllISRAcks, false)", defaultAcks, defaultDisable)
	}

	// Explicit new(1) (LeaderAck) must also construct cleanly without franz-go idempotency conflict.
	leaderClient, err := New(Config{
		Brokers: []string{"127.0.0.1:1"},
		Acks:    new(1),
	})
	if err != nil {
		t.Fatalf("New with Acks=new(1) failed: %v", err)
	}
	_ = leaderClient.Close()

	callerTLS := &tls.Config{ServerName: "kafka.internal", MinVersion: tls.VersionTLS13}
	client, err := New(Config{
		Brokers: []string{"127.0.0.1:1"},
		Acks:    new(0),
		TLS:     callerTLS,
		SASL:    &SASLConfig{Mechanism: "SCRAM-SHA-512", Username: "u", Password: "p"},
	})
	if err != nil {
		t.Fatalf("New with Acks=new(0) failed: %v", err)
	}

	callerTLS.ServerName = "mutated.example"

	ctx, cancel := context.WithTimeout(t.Context(), 50*time.Millisecond)
	defer cancel()
	if err := client.HealthCheck(ctx); err == nil {
		t.Fatal("expected HealthCheck to fail on unreachable broker")
	}
	if err := client.Stop(t.Context()); err != nil {
		t.Fatalf("Stop failed: %v", err)
	}
	if err := client.Close(); err != nil {
		t.Fatalf("idempotent Close failed: %v", err)
	}
}
