package config

// Loader defines the interface for configuration loaders
type Loader interface {
	// Load loads the configuration into the target
	Load(target any) error

	// Watch starts watching for configuration changes
	// The callback is invoked when configuration changes are detected
	Watch(callback func()) error
}
