package plugin

import "github.com/gin-gonic/gin"

// Webhooker is the interface plugin should implement to register custom handlers.
type Webhooker interface {
	Plugin
	// RegisterWebhook is called for plugins to create their own handler.
	// Plugins can call mux.BasePath() to acquire their custom handler base path.
	RegisterWebhook(mux gin.RouterGroup)
}
