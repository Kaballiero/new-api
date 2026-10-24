package taskruntime

import (
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	"github.com/QuantumNous/new-api/internal/adapter/gateway/taskplugins"
	gormtasks "github.com/QuantumNous/new-api/internal/adapter/gorm/tasks"
	"github.com/QuantumNous/new-api/internal/adapter/http/taskmedia"
	httptasks "github.com/QuantumNous/new-api/internal/adapter/http/tasks"
	runtimetechnical "github.com/QuantumNous/new-api/internal/adapter/taskruntime"
	"github.com/QuantumNous/new-api/internal/tasks/admin_list_midjourney_tasks"
	"github.com/QuantumNous/new-api/internal/tasks/admin_list_tasks"
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
)

type HTTP struct {
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
	reader := gormtasks.Reader{Store: gormtasks.NewStore(func() *gorm.DB { return model.DB })}
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
	return HTTP{
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
