package channels

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/QuantumNous/new-api/i18n"
	"github.com/QuantumNous/new-api/internal/channels/domain"
)

// ModelUpdateNotification phrases the upstream model update report in lang.
// At most 8 channels, 12 model samples of each kind and 10 failed channel IDs
// are listed.
func ModelUpdateNotification(lang string, report domain.ModelUpdateReport) (subject, content string) {
	var builder strings.Builder
	failedChannels := len(report.FailedChannelIDs)
	builder.WriteString(i18n.Translate(lang, i18n.MsgChannelModelUpdateSummary, map[string]any{
		"Checked":   report.CheckedChannels,
		"Changed":   report.ChangedChannels,
		"Added":     report.DetectedAddModels,
		"Removed":   report.DetectedRemoveModels,
		"AutoAdded": report.AutoAddedModels,
		"Failed":    failedChannels,
	}))

	if len(report.Channels) > 0 {
		displayCount := min(len(report.Channels), 8)
		builder.WriteString("\n\n" + i18n.Translate(lang, i18n.MsgChannelModelUpdateChannels, map[string]any{"Shown": displayCount, "Total": len(report.Channels)}))
		for _, summary := range report.Channels[:displayCount] {
			builder.WriteString(fmt.Sprintf("\n- %s (+%d / -%d)", summary.ChannelName, summary.AddCount, summary.RemoveCount))
		}
		if len(report.Channels) > displayCount {
			builder.WriteString("\n- " + i18n.Translate(lang, i18n.MsgChannelModelUpdateChannelsOmitted, map[string]any{"Count": len(report.Channels) - displayCount}))
		}
	}

	writeModelSamples(&builder, lang, i18n.MsgChannelModelUpdateAddSamples, report.AddModelSamples)
	writeModelSamples(&builder, lang, i18n.MsgChannelModelUpdateRemoveSamples, report.RemoveModelSamples)

	if failedChannels > 0 {
		displayCount := min(failedChannels, 10)
		displayIDs := make([]string, displayCount)
		for i, channelID := range report.FailedChannelIDs[:displayCount] {
			displayIDs[i] = strconv.Itoa(channelID)
		}
		builder.WriteString("\n\n" + i18n.Translate(lang, i18n.MsgChannelModelUpdateFailedIDs, map[string]any{"Shown": displayCount, "Total": failedChannels, "IDs": strings.Join(displayIDs, ", ")}))
		if failedChannels > displayCount {
			builder.WriteString(i18n.Translate(lang, i18n.MsgChannelModelUpdateOmitted, map[string]any{"Count": failedChannels - displayCount}))
		}
	}
	return i18n.Translate(lang, i18n.MsgChannelModelUpdateSubject), builder.String()
}

// writeModelSamples lists at most 12 normalized model names under key.
func writeModelSamples(builder *strings.Builder, lang, key string, models []string) {
	samples := domain.NormalizeModelNames(models)
	if len(samples) == 0 {
		return
	}
	displayCount := min(len(samples), 12)
	builder.WriteString("\n\n" + i18n.Translate(lang, key, map[string]any{"Shown": displayCount, "Total": len(samples), "Models": strings.Join(samples[:displayCount], ", ")}))
	if len(samples) > displayCount {
		builder.WriteString(i18n.Translate(lang, i18n.MsgChannelModelUpdateOmitted, map[string]any{"Count": len(samples) - displayCount}))
	}
}
