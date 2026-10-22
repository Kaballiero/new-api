// Package authentication assembles gateway and dashboard credential checks.
// It is separate from app's HTTP handlers, which depend on middleware.
package authentication

import (
	"github.com/QuantumNous/new-api/common"
	adapter "github.com/QuantumNous/new-api/internal/adapter/apikeys"
	"github.com/QuantumNous/new-api/internal/apikeys/validate_key"
	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting/ratio_setting"
)

func NewGatewayKeyAuth() validate_key.Usecase {
	return validate_key.NewUsecase(adapter.Authentication{}, validate_key.Rules{
		Now: common.GetTimestamp, CacheEnabled: func() bool { return common.RedisEnabled },
		UsableGroups: service.GetUserUsableGroups, GroupExists: ratio_setting.ContainsGroupRatio,
		IPAllowed: common.IsIpInCIDRList, Log: common.SysLog, Debug: logger.LogDebug,
	})
}
