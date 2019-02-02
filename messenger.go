package plugin

// Message describes a message to be send by MessageHandler#SendMessage
type Message struct {
	Message  string
	Title    string
	Priority int
	Extras   map[string]interface{}
}

// MessageHandler consists of message callbacks to be used by plugins.
type MessageHandler interface {
	// SendMessage sends a message with the given information in the request.
	SendMessage(msg Message) error
}

// Messenger is the interface plugins should implement to send messages.
type Messenger interface {
	Plugin
	// SetMessageHandler is called every time the plugin is initialized.
	// Plugins should record the handler and use the callbacks provided in the handler to send messages.
	SetMessageHandler(h MessageHandler)
}
