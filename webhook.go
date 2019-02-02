package plugin

import "github.com/gin-gonic/gin"

// Webhooker is the interface plugin should implement to register custom handlers.
type Webhooker interface {
	Plugin
	// RegisterWebhook is called for plugins to create their own handler.
	RegisterWebhook(basePath string, mux *gin.RouterGroup)
}
