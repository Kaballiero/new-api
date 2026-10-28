package app

import (
	"sync"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/internal/adapter/http/deployments"
	"github.com/QuantumNous/new-api/internal/deployments/check_deployment_name"
	"github.com/QuantumNous/new-api/internal/deployments/create_deployment"
	"github.com/QuantumNous/new-api/internal/deployments/domain"
	"github.com/QuantumNous/new-api/internal/deployments/estimate_deployment_price"
	"github.com/QuantumNous/new-api/internal/deployments/extend_deployment"
	"github.com/QuantumNous/new-api/internal/deployments/get_available_replicas"
	"github.com/QuantumNous/new-api/internal/deployments/get_deployment"
	"github.com/QuantumNous/new-api/internal/deployments/get_deployment_container"
	"github.com/QuantumNous/new-api/internal/deployments/get_deployment_logs"
	"github.com/QuantumNous/new-api/internal/deployments/get_deployment_settings"
	"github.com/QuantumNous/new-api/internal/deployments/list_deployment_containers"
	"github.com/QuantumNous/new-api/internal/deployments/list_deployment_hardware"
	"github.com/QuantumNous/new-api/internal/deployments/list_deployment_locations"
	"github.com/QuantumNous/new-api/internal/deployments/list_deployments"
	"github.com/QuantumNous/new-api/internal/deployments/rename_deployment"
	"github.com/QuantumNous/new-api/internal/deployments/search_deployments"
	"github.com/QuantumNous/new-api/internal/deployments/terminate_deployment"
	"github.com/QuantumNous/new-api/internal/deployments/test_deployment_connection"
	"github.com/QuantumNous/new-api/internal/deployments/update_deployment"
)

type DeploymentsHTTP struct {
	GetModelDeploymentSettings   get_deployment_settings.HTTP
	TestIoNetConnection          test_deployment_connection.HTTP
	GetAllDeployments            list_deployments.HTTP
	SearchDeployments            search_deployments.HTTP
	GetDeployment                get_deployment.HTTP
	UpdateDeploymentName         rename_deployment.HTTP
	UpdateDeployment             update_deployment.HTTP
	ExtendDeployment             extend_deployment.HTTP
	DeleteDeployment             terminate_deployment.HTTP
	CreateDeployment             create_deployment.HTTP
	GetHardwareTypes             list_deployment_hardware.HTTP
	GetLocations                 list_deployment_locations.HTTP
	GetAvailableReplicas         get_available_replicas.HTTP
	GetPriceEstimation           estimate_deployment_price.HTTP
	CheckClusterNameAvailability check_deployment_name.HTTP
	GetDeploymentLogs            get_deployment_logs.HTTP
	ListDeploymentContainers     list_deployment_containers.HTTP
	GetContainerDetails          get_deployment_container.HTTP
}

var deploymentsDefault = sync.OnceValue(newDeploymentsHTTP)

func DeploymentHandlers() DeploymentsHTTP { return deploymentsDefault() }
func newDeploymentsHTTP() DeploymentsHTTP {
	settings := func() domain.Settings {
		common.OptionMapRWMutex.RLock()
		defer common.OptionMapRWMutex.RUnlock()
		return domain.Settings{Enabled: common.OptionMap["model_deployment.ionet.enabled"] == "true", APIKey: common.OptionMap["model_deployment.ionet.api_key"]}
	}
	return DeploymentsHTTP{GetModelDeploymentSettings: get_deployment_settings.NewHTTP(settings),
		TestIoNetConnection:          test_deployment_connection.NewHTTP(settings, func(key string) test_deployment_connection.Provider { return deployments.Enterprise(key) }),
		GetAllDeployments:            list_deployments.NewHTTP(settings, list_deployments.NewUsecase(func(key string) list_deployments.Provider { return deployments.Enterprise(key) }, time.Now)),
		SearchDeployments:            search_deployments.NewHTTP(settings, search_deployments.NewUsecase(func(key string) search_deployments.Provider { return deployments.Enterprise(key) }, time.Now)),
		GetDeployment:                get_deployment.NewHTTP(settings, func(key string) get_deployment.Provider { return deployments.Enterprise(key) }),
		UpdateDeploymentName:         rename_deployment.NewHTTP(settings, rename_deployment.NewUsecase(func(key string) rename_deployment.Provider { return deployments.Enterprise(key) })),
		UpdateDeployment:             update_deployment.NewHTTP(settings, func(key string) update_deployment.Provider { return deployments.Enterprise(key) }),
		ExtendDeployment:             extend_deployment.NewHTTP(settings, extend_deployment.NewUsecase(func(key string) extend_deployment.Provider { return deployments.Enterprise(key) }, time.Now)),
		DeleteDeployment:             terminate_deployment.NewHTTP(settings, func(key string) terminate_deployment.Provider { return deployments.Enterprise(key) }),
		CreateDeployment:             create_deployment.NewHTTP(settings, func(key string) create_deployment.Provider { return deployments.Enterprise(key) }),
		GetHardwareTypes:             list_deployment_hardware.NewHTTP(settings, func(key string) list_deployment_hardware.Provider { return deployments.Enterprise(key) }),
		GetLocations:                 list_deployment_locations.NewHTTP(settings, func(key string) list_deployment_locations.Provider { return deployments.Public(key) }),
		GetAvailableReplicas:         get_available_replicas.NewHTTP(settings, func(key string) get_available_replicas.Provider { return deployments.Enterprise(key) }),
		GetPriceEstimation:           estimate_deployment_price.NewHTTP(settings, func(key string) estimate_deployment_price.Provider { return deployments.Enterprise(key) }),
		CheckClusterNameAvailability: check_deployment_name.NewHTTP(settings, func(key string) check_deployment_name.Provider { return deployments.Enterprise(key) }),
		GetDeploymentLogs:            get_deployment_logs.NewHTTP(settings, func(key string) get_deployment_logs.Provider { return deployments.Public(key) }),
		ListDeploymentContainers:     list_deployment_containers.NewHTTP(settings, func(key string) list_deployment_containers.Provider { return deployments.Enterprise(key) }),
		GetContainerDetails:          get_deployment_container.NewHTTP(settings, func(key string) get_deployment_container.Provider { return deployments.Enterprise(key) }),
	}
}
