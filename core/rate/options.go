package rate

import (
	"fmt"
	"strings"
	"unicode"
)

type options struct {
	keyPrefix string
}

// Option configures a limiter during construction.
type Option func(*options) error

// WithKeyPrefix sets the key prefix. Outer whitespace and colons are removed;
// a prefix that is empty after normalization is rejected. The default is "rate".
func WithKeyPrefix(prefix string) Option {
	prefix = strings.TrimFunc(prefix, func(r rune) bool {
		return r == ':' || unicode.IsSpace(r)
	})
	return func(o *options) error {
		if prefix == "" {
			return fmt.Errorf("%w: empty key prefix", ErrInvalidOption)
		}
		o.keyPrefix = prefix
		return nil
	}
}

func applyOptions(optionList []Option) (options, error) {
	result := options{keyPrefix: "rate"}
	for index, option := range optionList {
		if option == nil {
			return options{}, fmt.Errorf("%w: nil option at index %d", ErrInvalidOption, index)
		}
		if err := option(&result); err != nil {
			return options{}, err
		}
	}
	return result, nil
}
