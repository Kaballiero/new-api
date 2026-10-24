package controller

import "github.com/gin-gonic/gin"

// BRIDGE: video router entrypoint; removed when router binds the task module directly.
func VideoProxy(c *gin.Context) { taskHTTP().VideoProxy(c) }
