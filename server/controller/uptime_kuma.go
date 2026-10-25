package controller

import "github.com/gin-gonic/gin"

// BRIDGE: existing router registration; remove when router assembles the system handler directly.
func GetUptimeKumaStatus(c *gin.Context) { systemHTTP().Uptime.Handle(c) }
