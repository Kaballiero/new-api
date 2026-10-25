package controller

import (
	settingsapp "github.com/QuantumNous/new-api/internal/app/settings"

	"sync"

	"github.com/gin-gonic/gin"
)

// BRIDGE: existing option and public configuration router registrations; remove when router composes settings handlers.
var settingsHTTP = sync.OnceValue(settingsapp.NewHTTP)

func GetOptions(c *gin.Context)   { settingsHTTP().GetOptions.Handle(c) }
func UpdateOption(c *gin.Context) { settingsHTTP().UpdateOption.Handle(c) }
