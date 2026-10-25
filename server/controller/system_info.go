package controller

import "github.com/gin-gonic/gin"

// BRIDGE: existing router registration; remove when router assembles the system handler directly.
func ListSystemInstances(c *gin.Context) { systemHTTP().Instances.Handle(c) }

// BRIDGE: existing router registration; remove when router assembles the system handler directly.
func DeleteStaleSystemInstances(c *gin.Context) { systemHTTP().DeleteInstances.Handle(c) }

// BRIDGE: existing router registration; remove when router assembles the system handler directly.
func DeleteStaleSystemInstance(c *gin.Context) { systemHTTP().DeleteInstance.Handle(c) }
