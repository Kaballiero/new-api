package controller

import (
	"github.com/QuantumNous/new-api/internal/channels/validate_models"
	"github.com/gin-gonic/gin"
)

type ValidateModelsRequest = validate_models.ValidateModelsRequest
type ModelValidationResult = validate_models.ModelValidationResult
type ValidateModelsResponse = validate_models.ValidateModelsResponse

func ValidateModels(c *gin.Context)             { channelsHTTP().ValidateModels.Handle(c) }
func dedupeTrimModels(models []string) []string { return validate_models.NormalizeModels(models) }
