package mongo

import (
	"cmp"
	"crypto/tls"
	"errors"
	"fmt"
	"slices"
	"time"

	"go.mongodb.org/mongo-driver/v2/mongo/options"
	"go.opentelemetry.io/contrib/instrumentation/go.mongodb.org/mongo-driver/v2/mongo/otelmongo"
)

const (
	defaultAuthSource             = "admin"
	defaultConnectTimeout         = 10 * time.Second
	defaultServerSelectionTimeout = 5 * time.Second
)

// Config configures a MongoDB client.
type Config struct {
	URI                    string        `json:"uri"`
	Hosts                  []string      `json:"hosts"`
	Database               string        `json:"database"`
	Username               string        `json:"username"`
	Password               string        `json:"password"`
	AuthSource             string        `json:"auth_source"`
	ReplicaSet             string        `json:"replica_set"`
	MaxPoolSize            uint64        `json:"max_pool_size"`
	MinPoolSize            uint64        `json:"min_pool_size"`
	MaxConnIdleTime        time.Duration `json:"max_conn_idle_time"`
	ConnectTimeout         time.Duration `json:"connect_timeout"`
	ServerSelectionTimeout time.Duration `json:"server_selection_timeout"`
	TLS                    *tls.Config   `json:"-"`
	EnableTracing          *bool         `json:"enable_tracing"`
}

// Validate validates Config.
func (c Config) Validate() error {
	var errs []error
	if c.URI == "" && len(c.Hosts) == 0 {
		errs = append(errs, errors.New("mongo: either uri or at least one host is required"))
	}
	for i, host := range c.Hosts {
		if host == "" {
			errs = append(errs, fmt.Errorf("mongo: hosts[%d] cannot be empty", i))
		}
	}
	if c.MaxPoolSize > 0 && c.MinPoolSize > c.MaxPoolSize {
		errs = append(errs, errors.New("mongo: min_pool_size cannot exceed max_pool_size"))
	}
	if c.MaxConnIdleTime < 0 {
		errs = append(errs, errors.New("mongo: max_conn_idle_time cannot be negative"))
	}
	if c.ConnectTimeout < 0 {
		errs = append(errs, errors.New("mongo: connect_timeout cannot be negative"))
	}
	if c.ServerSelectionTimeout < 0 {
		errs = append(errs, errors.New("mongo: server_selection_timeout cannot be negative"))
	}
	return errors.Join(errs...)
}

// BuildClientOptions constructs *options.ClientOptions without forcing authentication on authless servers.
func (c Config) BuildClientOptions() (*options.ClientOptions, error) {
	if err := c.Validate(); err != nil {
		return nil, err
	}

	opts := options.Client()
	if c.URI != "" {
		opts.ApplyURI(c.URI)
	} else {
		opts.SetHosts(slices.Clone(c.Hosts))
	}

	// Only configure credentials when a username is supplied.
	if c.Username != "" {
		opts.SetAuth(options.Credential{
			Username:   c.Username,
			Password:   c.Password,
			AuthSource: cmp.Or(c.AuthSource, defaultAuthSource),
		})
	}
	if c.ReplicaSet != "" {
		opts.SetReplicaSet(c.ReplicaSet)
	}
	if c.MaxPoolSize > 0 {
		opts.SetMaxPoolSize(c.MaxPoolSize)
	}
	if c.MinPoolSize > 0 {
		opts.SetMinPoolSize(c.MinPoolSize)
	}
	if c.MaxConnIdleTime > 0 {
		opts.SetMaxConnIdleTime(c.MaxConnIdleTime)
	}
	if c.TLS != nil {
		opts.SetTLSConfig(c.TLS.Clone())
	}

	if c.ConnectTimeout > 0 {
		opts.SetConnectTimeout(c.ConnectTimeout)
	} else if opts.ConnectTimeout == nil {
		opts.SetConnectTimeout(defaultConnectTimeout)
	}
	if c.ServerSelectionTimeout > 0 {
		opts.SetServerSelectionTimeout(c.ServerSelectionTimeout)
	} else if opts.ServerSelectionTimeout == nil {
		opts.SetServerSelectionTimeout(defaultServerSelectionTimeout)
	}

	if c.EnableTracing == nil || *c.EnableTracing {
		opts.SetMonitor(otelmongo.NewMonitor())
	}
	if err := opts.Validate(); err != nil {
		return nil, fmt.Errorf("mongo: validate options: %w", err)
	}
	return opts, nil
}
