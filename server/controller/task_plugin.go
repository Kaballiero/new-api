package controller

import (
	httpapi "github.com/QuantumNous/new-api/internal/app/httpapi"

	"github.com/gin-gonic/gin"
)

func UploadTaskPlugin(c *gin.Context) { httpapi.UploadTaskPlugin().ServeHTTP(c) }

func ListTaskPlugins(c *gin.Context) { httpapi.ListTaskPlugins().ServeHTTP(c) }

func GetTaskPluginRuntime(c *gin.Context) { httpapi.GetTaskPluginRuntime().ServeHTTP(c) }

func GetTaskPlugin(c *gin.Context) { httpapi.GetTaskPlugin().ServeHTTP(c) }

func ActivateTaskPlugin(c *gin.Context) { httpapi.ActivateTaskPlugin().ServeHTTP(c) }

func DeleteTaskPluginVersion(c *gin.Context) { httpapi.DeleteTaskPluginVersion().ServeHTTP(c) }

func SetTaskPluginStatus(c *gin.Context) { httpapi.SetTaskPluginStatus().ServeHTTP(c) }

func UpdateTaskPluginMarketplaceSources(c *gin.Context) {
	httpapi.UpdateTaskPluginMarketplaceSources().ServeHTTP(c)
}

func GetTaskPluginOptions(c *gin.Context) { httpapi.GetTaskPluginOptions().ServeHTTP(c) }

func GetTaskPluginVersions(c *gin.Context) { httpapi.GetTaskPluginVersions().ServeHTTP(c) }

func GetTaskPluginMarketplaceSources(c *gin.Context) {
	httpapi.GetTaskPluginMarketplaceSources().ServeHTTP(c)
}

func GetTaskPluginIcon(c *gin.Context) { httpapi.GetTaskPluginIcon().ServeHTTP(c) }

func DryRunTaskPlugin(c *gin.Context) { httpapi.DryRunTaskPlugin().ServeHTTP(c) }
