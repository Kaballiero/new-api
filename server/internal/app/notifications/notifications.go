// Package notifications assembles independent delivery and lazy limiting.
package notifications

import (
	"sync"

	"github.com/QuantumNous/new-api/common"
	accountsadapter "github.com/QuantumNous/new-api/internal/adapter/gorm/accounts"
	delivery "github.com/QuantumNous/new-api/internal/adapter/http/notifications"
	"github.com/QuantumNous/new-api/internal/app/accountstorage"
	"github.com/QuantumNous/new-api/internal/notifications/domain"
	"github.com/QuantumNous/new-api/internal/notifications/notify_model_update_watchers"
	"github.com/QuantumNous/new-api/internal/notifications/notify_root_user"
	"github.com/QuantumNous/new-api/internal/notifications/send_notification"
	httpclient "github.com/QuantumNous/new-api/pkg/httpclient"
	"github.com/QuantumNous/new-api/setting/system_setting"
)

type runtime struct {
	send     send_notification.Usecase
	root     notify_root_user.Usecase
	watchers notify_model_update_watchers.Usecase
}

var assembled = sync.OnceValue(func() runtime {
	transport := delivery.Delivery{WorkerEnabled: system_setting.EnableWorker, WorkerKey: func() string { return system_setting.WorkerValidKey }, Worker: httpclient.DoWorkerRequest, Validate: httpclient.ValidateSSRFProtectedFetchURL, Client: httpclient.GetSSRFProtectedHTTPClient}
	send := send_notification.NewUsecase(checkNotificationLimit, transport, common.SysLog)
	recipients := accountsadapter.NotificationRecipients{SQL: accountstorage.SQL()}
	return runtime{send: send, root: notify_root_user.NewUsecase(recipients.Root, send.Execute, common.SysLog), watchers: notify_model_update_watchers.NewUsecase(recipients.Watchers, send.Execute, common.SysLog)}
})

func Send(userID int, email string, preferences domain.Preferences, notice domain.Notice) error {
	return assembled().send.Execute(userID, email, preferences, notice)
}
func Root(event, title, content string)         { _ = assembled().root.Execute(event, title, content) }
func ModelUpdateWatchers(title, content string) { _ = assembled().watchers.Execute(title, content) }
