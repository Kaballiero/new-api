package model

import (
	"github.com/QuantumNous/new-api/internal/catalog/list_missing_models"
)

func GetMissingModels() ([]string, error) {
	values, failure := list_missing_models.NewUsecase(catalogReadBridge{}).Execute()
	if failure != nil {
		return nil, failure.Cause
	}
	return values, nil
}
