package controller

import "github.com/gin-gonic/gin"

// BRIDGE: existing router registration; remove when router assembles the system handler directly.
func CreateLogCleanupSystemTask(c *gin.Context) { systemHTTP().StartCleanup.Handle(c) }

// BRIDGE: existing router registration; remove when router assembles the system handler directly.
func GetCurrentSystemTask(c *gin.Context) { systemHTTP().CurrentTask.Handle(c) }

// BRIDGE: existing router registration; remove when router assembles the system handler directly.
func ListSystemTasks(c *gin.Context) { systemHTTP().ListTasks.Handle(c) }

// BRIDGE: existing router registration; remove when router assembles the system handler directly.
func GetSystemTask(c *gin.Context) { systemHTTP().GetTask.Handle(c) }
