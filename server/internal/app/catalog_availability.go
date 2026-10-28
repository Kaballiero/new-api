package app

import (
	"sync"

	"github.com/QuantumNous/new-api/common"
	routing "github.com/QuantumNous/new-api/gateway/routing"
	gateway "github.com/QuantumNous/new-api/internal/adapter/gateway/catalog"
	accounts "github.com/QuantumNous/new-api/internal/adapter/gorm/accounts"
	store "github.com/QuantumNous/new-api/internal/adapter/gorm/catalog"
	channelstore "github.com/QuantumNous/new-api/internal/adapter/gorm/channels"
	"github.com/QuantumNous/new-api/internal/app/accountstorage"
	pricingruntime "github.com/QuantumNous/new-api/internal/app/pricingruntime"
	"github.com/QuantumNous/new-api/internal/catalog/filter_billable_models"
	"github.com/QuantumNous/new-api/internal/catalog/get_registered_model"
	"github.com/QuantumNous/new-api/internal/catalog/list_available_models"
	"github.com/QuantumNous/new-api/internal/catalog/list_model_presets"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/relay/modelview"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/QuantumNous/new-api/setting/ratio_setting"

	"gorm.io/gorm"
)

type CatalogAvailabilityHTTP struct {
	List     list_available_models.HTTP
	Retrieve get_registered_model.HTTP
	Presets  list_model_presets.HTTP
}

var catalogAvailabilityDefault = sync.OnceValue(newCatalogAvailabilityHTTP)

func CatalogModelHandlers() CatalogAvailabilityHTTP { return catalogAvailabilityDefault() }
func newCatalogAvailabilityHTTP() CatalogAvailabilityHTTP {
	models := gateway.Models{}
	availability := store.NewAvailabilityStore(func() *gorm.DB { return model.DB }, pricingruntime.NewCatalogPersistence().GroupColumn)
	billable := filter_billable_models.NewUsecase(availability, models.Configured, common.SysLog)
	scenario := list_available_models.NewUsecase(availability, accounts.ModelAvailabilityPreferences{SQL: accountstorage.SQL()}, models, list_available_models.Runtime{SelfUse: func() bool { return operation_setting.SelfUseModeEnabled }, EnabledModels: channelstore.GetGroupsEnabledModels, RoutingMatch: ratio_setting.RoutingMatchModelName, Log: common.SysLog, BillableModels: billable.Execute})
	return CatalogAvailabilityHTTP{List: list_available_models.NewHTTP(scenario, routing.GetRequestAutoGroups, modelview.List), Retrieve: get_registered_model.NewHTTP(models, modelview.Retrieve), Presets: list_model_presets.NewHTTP(models)}
}
