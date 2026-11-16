package channels

import (
	"fmt"
	"maps"
	"strconv"
	"strings"

	"github.com/QuantumNous/new-api/i18n"
	"github.com/QuantumNous/new-api/internal/channels/domain"
)

// ModelUpdateNotification phrases the upstream model update report in lang.
// At most 8 channels, 12 model samples of each kind and 10 failed channel IDs
// are listed. The models applied by auto sync and the pending removals of the
// channels over the auto removal limit are listed with their channel.
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

	writeModelSamples(&builder, lang, i18n.MsgChannelModelUpdateAddSamples, report.AddModelSamples, nil)
	writeModelSamples(&builder, lang, i18n.MsgChannelModelUpdateRemoveSamples, report.RemoveModelSamples, nil)
	var autoAddedSamples, autoRemovedSamples, removalLimitSamples, blockedChannels []string
	for _, summary := range report.Channels {
		if summary.AutoApplyBlocked {
			blockedChannels = append(blockedChannels, summary.ChannelName)
		}
		autoAddedSamples = append(autoAddedSamples, channelModelSamples(summary.ChannelName, summary.AutoAddedModels)...)
		autoRemovedSamples = append(autoRemovedSamples, channelModelSamples(summary.ChannelName, summary.AutoRemovedModels)...)
		if summary.RemovalLimitExceeded {
			removalLimitSamples = append(removalLimitSamples, channelModelSamples(summary.ChannelName, summary.PendingRemoveModels)...)
		}
	}
	writeModelSamples(&builder, lang, i18n.MsgChannelModelUpdateAutoAdded, autoAddedSamples, nil)
	writeModelSamples(&builder, lang, i18n.MsgChannelModelUpdateAutoRemoved, autoRemovedSamples, nil)
	writeModelSamples(&builder, lang, i18n.MsgChannelModelUpdateRemovalLimit, removalLimitSamples, map[string]any{"Limit": domain.MaxAutoRemovedModels})
	conflictSamples := make([]string, len(report.ConflictModels))
	for i, conflict := range report.ConflictModels {
		conflictSamples[i] = fmt.Sprintf("%s (%s)", conflict.Model, conflict.ChannelName)
	}
	writeModelSamples(&builder, lang, i18n.MsgChannelModelUpdateConflictSamples, conflictSamples, nil)
	writeModelSamples(&builder, lang, i18n.MsgChannelModelUpdateAutoApplyBlocked, blockedChannels, nil)

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

// channelModelSamples are the models of a channel as "model (channel)".
func channelModelSamples(channelName string, models []string) []string {
	samples := make([]string, len(models))
	for i, model := range models {
		samples[i] = fmt.Sprintf("%s (%s)", model, channelName)
	}
	return samples
}

// writeModelSamples lists at most 12 normalized model names under key; data
// holds the other values of the key's template.
func writeModelSamples(builder *strings.Builder, lang, key string, models []string, data map[string]any) {
	samples := domain.NormalizeModelNames(models)
	if len(samples) == 0 {
		return
	}
	displayCount := min(len(samples), 12)
	templateData := map[string]any{"Shown": displayCount, "Total": len(samples), "Models": strings.Join(samples[:displayCount], ", ")}
	maps.Copy(templateData, data)
	builder.WriteString("\n\n" + i18n.Translate(lang, key, templateData))
	if len(samples) > displayCount {
		builder.WriteString(i18n.Translate(lang, i18n.MsgChannelModelUpdateOmitted, map[string]any{"Count": len(samples) - displayCount}))
	}
}
