package taskruntime

import (
	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/internal/adapter/gateway/taskplugins"
	gormtasks "github.com/QuantumNous/new-api/internal/adapter/gorm/tasks"
	"github.com/QuantumNous/new-api/internal/adapter/http/taskmedia"
	httptasks "github.com/QuantumNous/new-api/internal/adapter/http/tasks"
	runtimetechnical "github.com/QuantumNous/new-api/internal/adapter/taskruntime"
	"github.com/QuantumNous/new-api/internal/app/accountstorage"
	auditapp "github.com/QuantumNous/new-api/internal/app/audit"
	tasksapp "github.com/QuantumNous/new-api/internal/app/tasks"
	"github.com/QuantumNous/new-api/internal/tasks/admin_list_midjourney_tasks"
	"github.com/QuantumNous/new-api/internal/tasks/admin_list_tasks"
	"github.com/QuantumNous/new-api/internal/tasks/apply_sync_operator_decision"
	"github.com/QuantumNous/new-api/internal/tasks/fetch_task_artifact"
	"github.com/QuantumNous/new-api/internal/tasks/fetch_video_content"
	"github.com/QuantumNous/new-api/internal/tasks/get_own_task"
	"github.com/QuantumNous/new-api/internal/tasks/list_own_midjourney_tasks"
	"github.com/QuantumNous/new-api/internal/tasks/list_own_tasks"
	"github.com/QuantumNous/new-api/internal/tasks/list_task_artifacts"
	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/middleware"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting"
	"github.com/QuantumNous/new-api/setting/system_setting"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

type HTTP struct {
	ReleaseOpenRouterSyncHold gin.HandlerFunc
	SettleOpenRouterSyncCost  gin.HandlerFunc
	GetTask                   gin.HandlerFunc
	GetTaskArtifacts          gin.HandlerFunc
	GetDashboardTaskArtifacts gin.HandlerFunc
	TaskArtifactContent       gin.HandlerFunc
	GetAllTask                gin.HandlerFunc
	GetUserTask               gin.HandlerFunc
	GetAllMidjourney          gin.HandlerFunc
	GetUserMidjourney         gin.HandlerFunc
	VideoProxy                gin.HandlerFunc
}

func NewHTTP() HTTP {
	reader := gormtasks.Reader{Store: gormtasks.NewStore(func() *gorm.DB { return model.DB }, accountstorage.Cache())}
	projector := taskplugins.Artifacts{}
	storage := httptasks.ArtifactStorage{}
	proxy := taskmedia.NewProxy()
	capability := middleware.IsTaskArtifactAccess
	own := list_own_tasks.NewHandler(list_own_tasks.NewUsecase(reader, runtimetechnical.ProjectClientTaskData))
	admin := admin_list_tasks.NewHandler(admin_list_tasks.NewUsecase(reader, runtimetechnical.ProjectClientTaskData))
	forwardEnabled := func() bool { return setting.MjForwardUrlEnabled }
	serverAddress := func() string { return system_setting.ServerAddress }
	mjOwn := list_own_midjourney_tasks.NewHandler(list_own_midjourney_tasks.NewUsecase(reader, forwardEnabled, serverAddress))
	mjAdmin := admin_list_midjourney_tasks.NewHandler(admin_list_midjourney_tasks.NewUsecase(reader, forwardEnabled, serverAddress))
	detail := get_own_task.NewHandler(get_own_task.NewUsecase(reader))
	artifacts := list_task_artifacts.NewHandler(list_task_artifacts.NewUsecase(reader, projector, storage), capability)
	contentUsecase := fetch_task_artifact.NewUsecase(reader, projector, projector, storage)
	content := fetch_task_artifact.NewHandler(contentUsecase, fetch_task_artifact.Delivery{
		Proxy: proxy.Serve, Stored: storage.Serve, LogError: logTaskMediaError, Capability: capability,
	})
	videoUsecase := fetch_video_content.NewUsecase(reader, projector, projector, httptasks.VideoDiagnostics{})
	video := fetch_video_content.NewHandler(videoUsecase, proxy.Serve, capability, logTaskMediaError)
	operator := apply_sync_operator_decision.NewHandler(tasksapp.NewReconciler().Operator, func(c *gin.Context, action, content string, params, rootFields map[string]any, status int, success bool) {
		auditapp.Recording().Operation(c, action, content, params, rootFields, status, success, false)
	}, common.SysLog)
	return HTTP{
		ReleaseOpenRouterSyncHold: operator.Release, SettleOpenRouterSyncCost: operator.Settle,
		GetTask:                   detail.Handle,
		GetTaskArtifacts:          artifacts.Handle,
		GetDashboardTaskArtifacts: artifacts.Dashboard,
		TaskArtifactContent:       content.Handle,
		GetAllTask:                admin.Handle,
		GetUserTask:               own.Handle,
		GetAllMidjourney:          mjAdmin.Handle,
		GetUserMidjourney:         mjOwn.Handle,
		VideoProxy:                video.Handle,
	}
}
func logTaskMediaError(c *gin.Context, err error) { logger.LogError(c.Request.Context(), err.Error()) }
