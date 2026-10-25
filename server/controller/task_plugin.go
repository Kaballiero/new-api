package controller

import (
	"sync"

	app "github.com/QuantumNous/new-api/internal/app/taskplugins"
	"github.com/gin-gonic/gin"
)

var handlerUploadTaskPlugin = sync.OnceValue(app.NewUploadTaskPlugin)

// BRIDGE: API router callers retain this entrypoint; remove when registrations use taskplugins transport directly.
func UploadTaskPlugin(c *gin.Context) { handlerUploadTaskPlugin().ServeHTTP(c) }

var handlerListTaskPlugins = sync.OnceValue(app.NewListTaskPlugins)

// BRIDGE: API router callers retain this entrypoint; remove when registrations use taskplugins transport directly.
func ListTaskPlugins(c *gin.Context) { handlerListTaskPlugins().ServeHTTP(c) }

var handlerGetTaskPluginRuntime = sync.OnceValue(app.NewGetTaskPluginRuntime)

// BRIDGE: API router callers retain this entrypoint; remove when registrations use taskplugins transport directly.
func GetTaskPluginRuntime(c *gin.Context) { handlerGetTaskPluginRuntime().ServeHTTP(c) }

var handlerGetTaskPlugin = sync.OnceValue(app.NewGetTaskPlugin)

// BRIDGE: API router callers retain this entrypoint; remove when registrations use taskplugins transport directly.
func GetTaskPlugin(c *gin.Context) { handlerGetTaskPlugin().ServeHTTP(c) }

var handlerActivateTaskPlugin = sync.OnceValue(app.NewActivateTaskPlugin)

// BRIDGE: API router callers retain this entrypoint; remove when registrations use taskplugins transport directly.
func ActivateTaskPlugin(c *gin.Context) { handlerActivateTaskPlugin().ServeHTTP(c) }

var handlerDeleteTaskPluginVersion = sync.OnceValue(app.NewDeleteTaskPluginVersion)

// BRIDGE: API router callers retain this entrypoint; remove when registrations use taskplugins transport directly.
func DeleteTaskPluginVersion(c *gin.Context) { handlerDeleteTaskPluginVersion().ServeHTTP(c) }

var handlerSetTaskPluginStatus = sync.OnceValue(app.NewSetTaskPluginStatus)

// BRIDGE: API router callers retain this entrypoint; remove when registrations use taskplugins transport directly.
func SetTaskPluginStatus(c *gin.Context) { handlerSetTaskPluginStatus().ServeHTTP(c) }

var handlerUpdateTaskPluginMarketplaceSources = sync.OnceValue(app.NewUpdateTaskPluginMarketplaceSources)

// BRIDGE: API router callers retain this entrypoint; remove when registrations use taskplugins transport directly.
func UpdateTaskPluginMarketplaceSources(c *gin.Context) {
	handlerUpdateTaskPluginMarketplaceSources().ServeHTTP(c)
}

var handlerGetTaskPluginOptions = sync.OnceValue(app.NewGetTaskPluginOptions)

// BRIDGE: API router callers retain this entrypoint; remove when registrations use taskplugins transport directly.
func GetTaskPluginOptions(c *gin.Context) { handlerGetTaskPluginOptions().ServeHTTP(c) }

var handlerGetTaskPluginVersions = sync.OnceValue(app.NewGetTaskPluginVersions)

// BRIDGE: API router callers retain this entrypoint; remove when registrations use taskplugins transport directly.
func GetTaskPluginVersions(c *gin.Context) { handlerGetTaskPluginVersions().ServeHTTP(c) }

var handlerGetTaskPluginMarketplaceSources = sync.OnceValue(app.NewGetTaskPluginMarketplaceSources)

// BRIDGE: API router callers retain this entrypoint; remove when registrations use taskplugins transport directly.
func GetTaskPluginMarketplaceSources(c *gin.Context) {
	handlerGetTaskPluginMarketplaceSources().ServeHTTP(c)
}

var handlerGetTaskPluginIcon = sync.OnceValue(app.NewGetTaskPluginIcon)

// BRIDGE: API router callers retain this entrypoint; remove when registrations use taskplugins transport directly.
func GetTaskPluginIcon(c *gin.Context) { handlerGetTaskPluginIcon().ServeHTTP(c) }

var handlerDryRunTaskPlugin = sync.OnceValue(app.NewDryRunTaskPlugin)

// BRIDGE: API router callers retain this entrypoint; remove when registrations use taskplugins transport directly.
func DryRunTaskPlugin(c *gin.Context) { handlerDryRunTaskPlugin().ServeHTTP(c) }
