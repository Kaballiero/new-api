// Package authentication assembles gateway and dashboard credential checks.

// It is separate from app's HTTP handlers, which depend on middleware.
package authentication

import (
	"github.com/QuantumNous/new-api/common"
	adapter "github.com/QuantumNous/new-api/internal/adapter/apikeys"
	accountssettings "github.com/QuantumNous/new-api/internal/adapter/settings/accounts"
	"github.com/QuantumNous/new-api/internal/apikeys/validate_key"
	"github.com/QuantumNous/new-api/internal/app/accountstorage"
	"github.com/QuantumNous/new-api/internal/app/apikeystorage"
	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/setting/ratio_setting"
)

func NewGatewayKeyAuth() validate_key.Usecase {
	return validate_key.NewUsecase(adapter.Authentication{Reader: apikeystorage.Reader(), Persistence: apikeystorage.Persistence(), ReadOwner: accountstorage.Cache().GetUserCache, Admin: accountstorage.SQL().IsAdmin}, validate_key.Rules{
		Now: common.GetTimestamp, CacheEnabled: func() bool { return common.RedisEnabled },
		UsableGroups: accountssettings.GetUserUsableGroups, GroupExists: ratio_setting.ContainsGroupRatio,
		IPAllowed: common.IsIpInCIDRList, Log: common.SysLog, Debug: logger.LogDebug,
	})
}
