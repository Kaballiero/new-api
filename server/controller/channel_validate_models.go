package controller

import (
	httpapi "github.com/QuantumNous/new-api/internal/app/httpapi"
	"github.com/QuantumNous/new-api/internal/channels/validate_models"
	"github.com/gin-gonic/gin"
)

type ValidateModelsRequest = validate_models.ValidateModelsRequest

type ModelValidationResult = validate_models.ModelValidationResult

type ValidateModelsResponse = validate_models.ValidateModelsResponse

func ValidateModels(c *gin.Context) { httpapi.Channels().ValidateModels.Handle(c) }
