package checkinsettings

import (
	"github.com/QuantumNous/new-api/internal/accounts/domain"
	"github.com/QuantumNous/new-api/setting/operation_setting"
)

// Current preserves the original live settings pointer, including late reads.
func Current() *domain.CheckinPolicy {
	return (*domain.CheckinPolicy)(operation_setting.GetCheckinSetting())
}
