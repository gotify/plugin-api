package plugin

import "net/url"

// Displayer is the interface plugins should implement to show a text to the user.
// The text will appear on the plugin details page and can be multi-line.
// Markdown syntax is allowed. Good for providing dynamically generated instructions to the user.
// Location is the current location the user is accessing the API from, nil if not recoverable.
type Displayer interface {
	Plugin
	GetDisplay(location *url.URL) string
}
