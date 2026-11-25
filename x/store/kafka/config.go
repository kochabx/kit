package kafka

import (
	"cmp"
	"crypto/tls"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"strings"
	"time"

	"github.com/twmb/franz-go/pkg/kgo"
	"github.com/twmb/franz-go/pkg/sasl"
	"github.com/twmb/franz-go/pkg/sasl/plain"
	"github.com/twmb/franz-go/pkg/sasl/scram"
	"github.com/twmb/franz-go/plugin/kotel"
	"github.com/twmb/franz-go/plugin/kslog"
)

const (
	defaultDialTimeout    = 5 * time.Second
	defaultRequestTimeout = 10 * time.Second
)

// SASLConfig configures SASL authentication for Kafka brokers.
type SASLConfig struct {
	Mechanism string `json:"mechanism"`
	Username  string `json:"username"`
	Password  string `json:"password"`
}

// Config configures a franz-go Kafka client.
type Config struct {
	Brokers                []string      `json:"brokers"`
	ClientID               string        `json:"client_id"`
	Group                  string        `json:"group"`
	Topics                 []string      `json:"topics"`
	Acks                   *int          `json:"acks"`
	DisableIdempotentWrite bool          `json:"disable_idempotent_write"`
	RequestTimeout         time.Duration `json:"request_timeout"`
	DialTimeout            time.Duration `json:"dial_timeout"`
	TLS                    *tls.Config   `json:"-"`
	SASL                   *SASLConfig   `json:"sasl"`
	EnableTracing          *bool         `json:"enable_tracing"`
}

// Validate validates Config.
func (c Config) Validate() error {
	var errs []error
	if len(c.Brokers) == 0 {
		errs = append(errs, errors.New("kafka: at least one broker is required"))
	}
	for i, b := range c.Brokers {
		if strings.TrimSpace(b) == "" {
			errs = append(errs, fmt.Errorf("kafka: brokers[%d] cannot be empty", i))
		}
	}
	for i, topic := range c.Topics {
		if strings.TrimSpace(topic) == "" {
			errs = append(errs, fmt.Errorf("kafka: topics[%d] cannot be empty", i))
		}
	}
	if c.Acks != nil {
		switch *c.Acks {
		case -1, 0, 1:
		default:
			errs = append(errs, fmt.Errorf("kafka: invalid acks %d (want -1, 0, or 1)", *c.Acks))
		}
	}
	if c.RequestTimeout < 0 {
		errs = append(errs, errors.New("kafka: request_timeout cannot be negative"))
	}
	if c.DialTimeout < 0 {
		errs = append(errs, errors.New("kafka: dial_timeout cannot be negative"))
	}
	if c.SASL != nil {
		switch strings.ToUpper(c.SASL.Mechanism) {
		case "PLAIN", "SCRAM-SHA-256", "SCRAM-SHA-512":
		default:
			errs = append(errs, fmt.Errorf("kafka: unsupported sasl mechanism %q", c.SASL.Mechanism))
		}
		if c.SASL.Username == "" {
			errs = append(errs, errors.New("kafka: sasl.username is required"))
		}
		if c.SASL.Password == "" {
			errs = append(errs, errors.New("kafka: sasl.password is required"))
		}
	}
	return errors.Join(errs...)
}

func (c Config) resolveAcks() (acks kgo.Acks, disableIdempotent bool) {
	if c.Acks == nil {
		return kgo.AllISRAcks(), c.DisableIdempotentWrite
	}
	switch *c.Acks {
	case 0:
		// franz-go requires disabling idempotent writes when NoAck (0) is selected.
		return kgo.NoAck(), true
	case 1:
		// franz-go requires disabling idempotent writes unless acks=all (-1).
		return kgo.LeaderAck(), true
	default:
		return kgo.AllISRAcks(), c.DisableIdempotentWrite
	}
}

func (c Config) saslMechanism() (sasl.Mechanism, error) {
	if c.SASL == nil {
		return nil, nil
	}
	switch strings.ToUpper(c.SASL.Mechanism) {
	case "PLAIN":
		return plain.Auth{User: c.SASL.Username, Pass: c.SASL.Password}.AsMechanism(), nil
	case "SCRAM-SHA-256":
		return scram.Auth{User: c.SASL.Username, Pass: c.SASL.Password}.AsSha256Mechanism(), nil
	case "SCRAM-SHA-512":
		return scram.Auth{User: c.SASL.Username, Pass: c.SASL.Password}.AsSha512Mechanism(), nil
	default:
		return nil, fmt.Errorf("kafka: unsupported sasl mechanism %q", c.SASL.Mechanism)
	}
}

// BuildKgoOpts translates Config into []kgo.Opt.
func (c Config) BuildKgoOpts(logger *slog.Logger) ([]kgo.Opt, error) {
	if err := c.Validate(); err != nil {
		return nil, err
	}

	kopts := []kgo.Opt{
		kgo.SeedBrokers(slices.Clone(c.Brokers)...),
		kgo.DialTimeout(cmp.Or(c.DialTimeout, defaultDialTimeout)),
		kgo.RequestTimeoutOverhead(cmp.Or(c.RequestTimeout, defaultRequestTimeout)),
	}
	if logger != nil {
		kopts = append(kopts, kgo.WithLogger(kslog.New(logger)))
	}
	if c.ClientID != "" {
		kopts = append(kopts, kgo.ClientID(c.ClientID))
	}
	if c.Group != "" {
		kopts = append(kopts, kgo.ConsumerGroup(c.Group))
	}
	if len(c.Topics) > 0 {
		kopts = append(kopts, kgo.ConsumeTopics(slices.Clone(c.Topics)...))
	}

	acks, disableIdempotent := c.resolveAcks()
	kopts = append(kopts, kgo.RequiredAcks(acks))
	if disableIdempotent {
		kopts = append(kopts, kgo.DisableIdempotentWrite())
	}

	if c.TLS != nil {
		kopts = append(kopts, kgo.DialTLSConfig(c.TLS.Clone()))
	}
	if mech, err := c.saslMechanism(); err != nil {
		return nil, err
	} else if mech != nil {
		kopts = append(kopts, kgo.SASL(mech))
	}

	if c.EnableTracing == nil || *c.EnableTracing {
		var tracerOpts []kotel.TracerOpt
		if c.ClientID != "" {
			tracerOpts = append(tracerOpts, kotel.ClientID(c.ClientID))
		}
		if c.Group != "" {
			tracerOpts = append(tracerOpts, kotel.ConsumerGroup(c.Group))
		}
		ko := kotel.NewKotel(
			kotel.WithTracer(kotel.NewTracer(tracerOpts...)),
			kotel.WithMeter(kotel.NewMeter()),
		)
		kopts = append(kopts, kgo.WithHooks(ko.Hooks()...))
	}

	return kopts, nil
}
