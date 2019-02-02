package plugin

// Configurer is the interface plugins should implement in order to provide configuration interface to the user
type Configurer interface {
	Plugin
	// DefaultConfig will be called on plugin first run to set the default config.
	// DefaultConfig will also be used if the provided config cannot be validate during initialization.
	// The default configuration will be provided to the user for future editing. Used for generating schemas and unmarshaling.
	DefaultConfig() interface{}
	// ValidateAndSetConfig will be called every time the plugin is initialized or the configuration has been changed by the user.
	// Plugins should check whether the configuration is valid and optionally return an error.
	// Parameter is guaranteed to be the same type as the return type of DefaultConfig()
	ValidateAndSetConfig(c interface{}) error
}
