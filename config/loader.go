package config

// Loader reads configuration and reports changes to its source.
type Loader interface {
	// Load decodes the current configuration into target.
	Load(target any) error

	// Watch invokes callback when the configuration source changes.
	Watch(callback func()) error
}
