package taskruntime

import (
	"regexp"

	"github.com/QuantumNous/new-api/i18n"
)

// TaskText formats host-produced task messages for their recipient. Provider
// failure strings are never passed through an error-string translator.
type TaskText struct{ Language func(int) string }

func (t TaskText) Phrase(userID int, key string, data map[string]any) string {
	return i18n.Translate(i18n.ParseAcceptLanguage(t.Language(userID)), key, data)
}
func (t TaskText) Timeout(userID, minutes int) string {
	return t.Phrase(userID, i18n.MsgTasksTimeout, map[string]any{"Minutes": minutes})
}
func (t TaskText) LegacyTimeout(userID int) string {
	return t.Phrase(userID, i18n.MsgTasksLegacyTimeout, nil)
}
func (t TaskText) MidjourneyTimeout(userID int) string {
	return t.Phrase(userID, i18n.MsgTasksMJTimeout, nil)
}
func (t TaskText) MidjourneyFailure(userID int) string {
	return t.Phrase(userID, i18n.MsgTasksMJFailure, nil)
}

var taskTokenSettlement = regexp.MustCompile(`^(?:Token recalculation: |token重算：)tokens=([0-9]+), modelRatio=(-?(?:[0-9]+\.[0-9]{2}|Inf)|NaN|\+Inf), groupRatio=(-?(?:[0-9]+\.[0-9]{2}|Inf)|NaN|\+Inf), otherMultiplier=(-?(?:[0-9]+\.[0-9]{4}|Inf)|NaN|\+Inf)$`)

// Adjustment handles only the host's known quota-adjustment descriptions. It is
// called after billing-stage selection, so translation cannot change metadata.
func (t TaskText) Adjustment(userID int, reason string) string {
	switch reason {
	case "任务用量表达式结算", "Task usage expression settlement":
		return t.Phrase(userID, i18n.MsgTasksTieredSettlement, nil)
	case "adaptor计费调整", "Adaptor billing adjustment":
		return t.Phrase(userID, i18n.MsgTasksAdaptorAdjustment, nil)
	}
	if fields := taskTokenSettlement.FindStringSubmatch(reason); fields != nil {
		return t.Phrase(userID, i18n.MsgTasksTokenSettlement, map[string]any{"Tokens": fields[1], "ModelRatio": fields[2], "GroupRatio": fields[3], "OtherMultiplier": fields[4]})
	}
	return reason
}
