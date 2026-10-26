package accountsadapter

import "github.com/QuantumNous/new-api/model"

type ModelAvailabilityPreferences struct{}

func (ModelAvailabilityPreferences) Group(id int) (string, error) {
	return model.GetUserGroup(id, false)
}
func (ModelAvailabilityPreferences) AcceptUnset(id int) bool {
	preferences, _ := model.GetUserSetting(id, false)
	return preferences.AcceptUnsetRatioModel
}
