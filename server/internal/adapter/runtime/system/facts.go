package system

import (
	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/setting/operation_setting"
)

type Facts struct{}

func (Facts) Version() string     { return common.Version }
func (Facts) StartTime() int64    { return common.StartTime }
func (Facts) Setup() bool         { return constant.Setup }
func (Facts) SetSetup(value bool) { constant.Setup = value }
func (Facts) Database() string    { return string(common.MainDatabaseType()) }
func (Facts) SetModes(self, demo bool) {
	operation_setting.SelfUseModeEnabled = self
	operation_setting.DemoSiteEnabled = demo
}
