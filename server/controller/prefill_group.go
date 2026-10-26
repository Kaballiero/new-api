package controller

import (
	"github.com/QuantumNous/new-api/internal/app"
	"github.com/gin-gonic/gin"
)

// BRIDGE: GetPrefillGroups serves existing router registrations; remove after direct router assembly of prefill handlers.
func GetPrefillGroups(c *gin.Context) { app.PrefillHandlers().List.Handle(c) }

// BRIDGE: CreatePrefillGroup serves existing router registrations; remove after direct router assembly of prefill handlers.
func CreatePrefillGroup(c *gin.Context) { app.PrefillHandlers().Create.Handle(c) }

// BRIDGE: UpdatePrefillGroup serves existing router registrations; remove after direct router assembly of prefill handlers.
func UpdatePrefillGroup(c *gin.Context) { app.PrefillHandlers().Update.Handle(c) }

// BRIDGE: DeletePrefillGroup serves existing router registrations; remove after direct router assembly of prefill handlers.
func DeletePrefillGroup(c *gin.Context) { app.PrefillHandlers().Delete.Handle(c) }
