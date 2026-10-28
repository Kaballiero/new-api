package controller

import (
	"github.com/QuantumNous/new-api/internal/app"
	"github.com/gin-gonic/gin"
)

func GetPrefillGroups(c *gin.Context) { app.PrefillHandlers().List.Handle(c) }

func CreatePrefillGroup(c *gin.Context) { app.PrefillHandlers().Create.Handle(c) }

func UpdatePrefillGroup(c *gin.Context) { app.PrefillHandlers().Update.Handle(c) }

func DeletePrefillGroup(c *gin.Context) { app.PrefillHandlers().Delete.Handle(c) }
