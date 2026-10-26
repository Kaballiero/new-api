package accountsadapter

import (
	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/relaykit/dto"
)

// SidebarModules preserves partial decoding and diagnostics of the stored profile settings.
func SidebarModules(raw string) string {
	setting := dto.UserSetting{}
	if raw != "" {
		if err := common.Unmarshal([]byte(raw), &setting); err != nil {
			common.SysLog("failed to unmarshal setting: " + err.Error())
		}
	}
	return setting.SidebarModules
}
