package kafka

import (
	"fmt"

	segmentio "github.com/segmentio/kafka-go"
)

// Option customizes advanced Kafka dependencies.
type Option func(*clientOptions) error

type clientOptions struct {
	dialer          *segmentio.Dialer
	transport       segmentio.RoundTripper
	asyncCompletion func([]segmentio.Message, error)
}

// WithAsyncCompletion handles delivery results from every asynchronous
// producer owned by the Client.
func WithAsyncCompletion(completion func([]segmentio.Message, error)) Option {
	return func(options *clientOptions) error {
		if completion == nil {
			return fmt.Errorf("%w: async completion is nil", ErrInvalidOption)
		}
		options.asyncCompletion = completion
		return nil
	}
}

// WithDialer replaces the dialer used by consumers and health checks.
func WithDialer(dialer *segmentio.Dialer) Option {
	return func(options *clientOptions) error {
		if dialer == nil {
			return fmt.Errorf("%w: dialer is nil", ErrInvalidOption)
		}
		options.dialer = dialer
		return nil
	}
}

// WithTransport replaces the transport used by producers.
func WithTransport(transport segmentio.RoundTripper) Option {
	return func(options *clientOptions) error {
		if transport == nil {
			return fmt.Errorf("%w: transport is nil", ErrInvalidOption)
		}
		options.transport = transport
		return nil
	}
}
