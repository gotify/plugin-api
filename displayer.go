package plugin

// Displayer is the interface plugins should implement to show a text to the user.
// The text will appear on the plugin details page and can be multi-line.
// Markdown syntax is allowed. Good for providing dynamically generated instructions to the user.
type Displayer interface {
	Plugin
	GetDisplay() string
}
