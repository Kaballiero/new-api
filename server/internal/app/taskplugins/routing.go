package taskplugins

import (
	"sync"

	"github.com/QuantumNous/new-api/constant"
	technical "github.com/QuantumNous/new-api/internal/adapter/taskplugins"
	tasksapp "github.com/QuantumNous/new-api/internal/app/tasks"
	"github.com/QuantumNous/new-api/internal/taskplugins/query_plugin_tasks"
	"github.com/QuantumNous/new-api/internal/taskplugins/resolve_plugin_origins"
)

var Origins = sync.OnceValue(func() resolve_plugin_origins.Usecase {
	return resolve_plugin_origins.NewUsecase(technical.TaskReader{Store: tasksapp.NewStore()},
		constant.ChannelTypeTaskPlugin,
		string(constant.TaskPlatformSuno))
})
var Queries = sync.OnceValue(func() query_plugin_tasks.Usecase {
	return query_plugin_tasks.NewUsecase(technical.TaskReader{Store: tasksapp.NewStore()},
		technical.TaskProjector{},
		constant.ChannelTypeTaskPlugin,
		string(constant.TaskPlatformSuno))
})
