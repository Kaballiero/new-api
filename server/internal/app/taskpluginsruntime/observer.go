package taskpluginsruntime

import (
	"sync"

	"github.com/QuantumNous/new-api/internal/taskplugins/domain"
	"github.com/QuantumNous/new-api/internal/taskplugins/observe_plugin_task"
	"github.com/QuantumNous/new-api/internal/taskplugins/retrieve_plugin_response"
)

var ObservationAdmissions = sync.OnceValue(func() *domain.ObservationLimiter {
	return domain.NewObservationLimiter(domain.DefaultObservationLimits)
})

var Observer = observe_plugin_task.Usecase{}

var Retriever = retrieve_plugin_response.Usecase{}
