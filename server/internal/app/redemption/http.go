// Package redemption assembles code management without a dependency on gateway middleware.
package redemption

import (
	"context"
	"fmt"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	"github.com/QuantumNous/new-api/common"
	storage "github.com/QuantumNous/new-api/internal/adapter/gorm/redemption"
	auditapp "github.com/QuantumNous/new-api/internal/app/audit"
	"github.com/QuantumNous/new-api/internal/redemption/create_codes"
	"github.com/QuantumNous/new-api/internal/redemption/delete_code"
	"github.com/QuantumNous/new-api/internal/redemption/delete_codes"
	"github.com/QuantumNous/new-api/internal/redemption/delete_invalid_codes"
	"github.com/QuantumNous/new-api/internal/redemption/domain"
	"github.com/QuantumNous/new-api/internal/redemption/get_code"
	"github.com/QuantumNous/new-api/internal/redemption/list_codes"
	"github.com/QuantumNous/new-api/internal/redemption/redeem_code"
	"github.com/QuantumNous/new-api/internal/redemption/search_codes"
	"github.com/QuantumNous/new-api/internal/redemption/update_code"
	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting/operation_setting"
)

type HTTP struct {
	List          list_codes.HTTP
	Search        search_codes.HTTP
	Get           get_code.HTTP
	Create        create_codes.HTTP
	Update        update_code.HTTP
	Delete        delete_code.HTTP
	DeleteBatch   delete_codes.HTTP
	DeleteInvalid delete_invalid_codes.HTTP
	Redeem        redeem_code.HTTP
}

func walletQuota(quota int) error {
	if err := common.ValidateWalletQuota(quota); err != nil {
		return domain.ErrWalletQuota
	}
	return nil
}

// Redeemer wires the single credit transaction and the post-commit cache and receipt ports.
func Redeemer() redeem_code.Usecase {
	store := storage.NewStore(func() *gorm.DB { return model.DB }, model.LockForUpdate, model.CreditTopUpQuota)
	return redeem_code.NewUsecase(store, receipts{}, func(user, quota int) { model.SyncCreditUserQuotaCache(user, quota, "redemption") },
		func(err error) { common.SysError("redemption failed: " + err.Error()) }, redeem_code.Timing{Now: common.GetTimestamp, Wait: common.RandomSleep})
}

// NewHTTP wires admission, independent management operations and audit callbacks outside their writes.
func NewHTTP() HTTP {
	store := storage.NewStore(func() *gorm.DB { return model.DB }, model.LockForUpdate, model.CreditTopUpQuota)
	recording := auditapp.NewRecording()
	return HTTP{
		List:   list_codes.NewHTTP(list_codes.NewUsecase(store)),
		Search: search_codes.NewHTTP(search_codes.NewUsecase(store, common.GetTimestamp)),
		Get:    get_code.NewHTTP(get_code.NewUsecase(store)),
		Create: create_codes.NewHTTP(create_codes.NewUsecase(store, walletQuota, common.GetTimestamp, common.GetUUID),
			operation_setting.IsPaymentComplianceConfirmed, func(c *gin.Context, in create_codes.Input) {
				recording.Manage(c, 0, "redemption.create", map[string]any{
					"name": in.Name, "count": in.Count, "quota": quotaText(in.Quota, receiptLanguage(in.IssuerID)),
				})
			}, func(err error) { common.SysError("failed to insert redemption: " + err.Error()) }),
		Update: update_code.NewHTTP(update_code.NewUsecase(store, walletQuota, common.GetTimestamp)),
		Delete: delete_code.NewHTTP(delete_code.NewUsecase(store)),
		DeleteBatch: delete_codes.NewHTTP(delete_codes.NewUsecase(store), func(c *gin.Context, ids []int, count int64) {
			recording.Manage(c, 0, "redemption.delete_batch", map[string]any{"count": count, "total": len(ids), "requested_redemption_ids": ids})
		}),
		DeleteInvalid: delete_invalid_codes.NewHTTP(delete_invalid_codes.NewUsecase(store, common.GetTimestamp)),
		Redeem:        redeem_code.NewHTTP(Redeemer(), operation_setting.IsPaymentComplianceConfirmed, &admission{}, logRedemptionFailure),
	}
}

// The existing operational log includes the submitted code; changing that policy requires a separate security fix.
func logRedemptionFailure(c *gin.Context, key string, user int, err error) {
	var ctx context.Context = context.Background()
	if c != nil && c.Request != nil {
		ctx = c.Request.Context()
	}
	logger.LogError(ctx, fmt.Sprintf("failed to redeem key %s for user %d: %s", key, user, err.Error()))
}
