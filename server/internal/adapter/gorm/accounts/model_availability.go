package accountsadapter

import core "github.com/QuantumNous/new-api/internal/adapter/gorm/accounts/core"

type ModelAvailabilityPreferences struct{ SQL core.Store }

func (store ModelAvailabilityPreferences) Group(id int) (string, error) {
	return store.SQL.GetUserGroup(id, false)
}
func (store ModelAvailabilityPreferences) AcceptUnset(id int) bool {
	preferences, _ := store.SQL.GetUserSetting(id, false)
	return preferences.AcceptUnsetRatioModel
}
