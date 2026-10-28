// Package audit assembles audit dependencies without importing gateway middleware.
package audit

import (
	"context"
	"fmt"
	"sync"

	"github.com/QuantumNous/new-api/common"
	storage "github.com/QuantumNous/new-api/internal/adapter/gorm/audit"
	accountstorage "github.com/QuantumNous/new-api/internal/app/accountstorage"
	"github.com/QuantumNous/new-api/internal/audit/admin_list_events"
	"github.com/QuantumNous/new-api/internal/audit/domain"
	"github.com/QuantumNous/new-api/internal/audit/list_own_events"
	"github.com/QuantumNous/new-api/internal/audit/record_event"
	"github.com/QuantumNous/new-api/internal/audit/transport"
	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/model"

	"gorm.io/gorm"
)

type diagnostics struct{}

func (diagnostics) InvalidRole(ctx context.Context, requestID string, role int) {
	logger.LogError(ctx, fmt.Sprintf("audit actor role unavailable (request_id=%s, actor_role=%d)", requestID, role))
}

func store() storage.Store {
	return storage.NewStore(func() *gorm.DB { return model.LOG_DB }, func(id int) string {
		username, _ := accountstorage.SQL().GetUsernameById(id, false)
		return username
	})
}

func roles() domain.Roles {
	return domain.Roles{Common: common.RoleCommonUser, Admin: common.RoleAdminUser, Root: common.RoleRootUser}
}

func NewHTTP() transport.ReadHandlers {
	reader := store()
	admin := admin_list_events.NewHandler(admin_list_events.NewUsecase(reader, roles()))
	own := list_own_events.NewHandler(list_own_events.NewUsecase(reader, roles()))
	return transport.NewReadHandlers(admin.Handle, own.Handle)
}

func NewRecording() transport.Recording {
	usecase := record_event.NewUsecase(store(), diagnostics{}, roles(), common.GetTimestamp, common.NewRequestId)
	return transport.NewRecording(usecase.Execute, func(ctx context.Context, err error) { logger.LogError(ctx, err.Error()) },
		common.GetTimestamp, common.NewRequestId)
}

// Recording assembles the shared audit writer with late storage readers.
var Recording = sync.OnceValue(NewRecording)
