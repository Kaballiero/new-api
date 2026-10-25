package controller

import "github.com/gin-gonic/gin"

// BRIDGE: existing router registration; remove when router assembles the system handler directly.
func GetSetup(c *gin.Context) { systemHTTP().GetSetup.Handle(c) }

// BRIDGE: existing router registration; remove when router assembles the system handler directly.
func PostSetup(c *gin.Context) { systemHTTP().CompleteSetup.Handle(c) }
