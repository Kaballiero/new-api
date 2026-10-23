package transport

import (
	"errors"

	"github.com/gin-gonic/gin"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/i18n"
	"github.com/QuantumNous/new-api/internal/redemption/domain"
)

func WriteError(c *gin.Context, err error) {
	key := ""
	switch {
	case errors.Is(err, domain.ErrIDEmpty):
		key = i18n.MsgRedemptionIDEmpty
	case errors.Is(err, domain.ErrNameLength):
		key = i18n.MsgRedemptionNameLength
	case errors.Is(err, domain.ErrCountPositive):
		key = i18n.MsgRedemptionCountPositive
	case errors.Is(err, domain.ErrCountMax):
		key = i18n.MsgRedemptionCountMax
	case errors.Is(err, domain.ErrQuotaPositive):
		key = i18n.MsgRedemptionQuotaPositive
	case errors.Is(err, domain.ErrWalletQuota):
		common.ApiErrorI18n(c, i18n.MsgRedemptionWalletQuota, map[string]any{"Max": common.MaxWalletQuota})
		return
	case errors.Is(err, domain.ErrExpiry):
		key = i18n.MsgRedemptionExpireTimeInvalid
	}
	if key != "" {
		common.ApiErrorI18n(c, key)
		return
	}
	common.ApiError(c, err)
}
