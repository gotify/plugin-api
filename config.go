package plugin

// Configurer is the interface plugins should implement in order to provide configuration interface to the user
type Configurer interface {
	Plugin
	// DefaultConfig will be called on plugin first run. The default configuration will be provided to the user for future editing.
	DefaultConfig() interface{}
	// EmptyConfig returns an instance of an empty configuration. Used for generating schemes.
	EmptyConfig() interface{}
	// ValidateAndSetConfig will be called every time the plugin is initialized or the configuration has been changed by the user.
	// Plugins should check whether the configuration is valid and optionally return an error.
	// Parameter is guaranteed to be the same type as the return type of EmptyConfig()
	ValidateAndSetConfig(c interface{}) error
}
