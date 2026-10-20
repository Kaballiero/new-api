package controller

import (
	repository "github.com/QuantumNous/new-api/internal/adapter/gorm/channels"
	"github.com/QuantumNous/new-api/internal/app"
	"github.com/QuantumNous/new-api/internal/channels/validate_models"
	"github.com/QuantumNous/new-api/model"
	"github.com/gin-gonic/gin"

	"time"
)

const (
	validateModelsMaxModels       = 1000
	validateModelsConcurrency     = 8
	validateModelsPerModelTimeout = 20 * time.Second
)

type ValidateModelsRequest = validate_models.ValidateModelsRequest
type ModelValidationResult = validate_models.ModelValidationResult
type validateModelsSummary = validate_models.Summary
type ValidateModelsResponse = validate_models.ValidateModelsResponse

func ValidateModels(c *gin.Context) { app.NewValidateModelsHTTP(c, channelEffects()).Handle(c) }
func resolveValidateChannel(c *gin.Context, req *ValidateModelsRequest) (*model.Channel, bool) {
	channel, failure := app.NewModelValidationUsecase(c, channelEffects()).Resolve(req)
	if failure != nil {
		validate_models.WriteFailure(c, failure)
		return nil, false
	}
	return repository.FromDomain(channel), true
}
func validateOneModel(channel *model.Channel, userID int, modelName, endpoint string) ModelValidationResult {
	return app.NewModelValidationUsecase(nil, channelEffects()).ValidateOne(repository.ToDomain(channel), userID, modelName, endpoint)
}
func dedupeTrimModels(models []string) []string { return validate_models.NormalizeModels(models) }
