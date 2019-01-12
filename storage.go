package plugin

// StorageHandler consists callbacks used to perform read/writes to the persistent storage for plugins.
type StorageHandler interface {
	Save(b []byte) error
	Load() ([]byte, error)
}

// Storager is the interface plugins should implement to use persistent storage.
type Storager interface {
	Plugin
	SetStorageHandler(handler StorageHandler)
}
