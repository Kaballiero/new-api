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
// are listed. The models applied by auto sync are listed with their channel,
// the channels over the auto removal limit with their number of auto
// removals, the additions with a refused reserve with their channel, and the
// channels whose unresolved model mapping blocks auto-apply by id and name.
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
	// An unresolved mapping blocks the catalog before its model loss is
	// compared, so the blocked channels are listed only without one.
	var autoAddedSamples, autoRemovedSamples, removalLimitChannels, reserveRefusedSamples, blockedChannels []string
	for _, summary := range report.Channels {
		if summary.AutoApplyBlocked && len(report.UnresolvedMappingChannels) == 0 {
			blockedChannels = append(blockedChannels, summary.ChannelName)
		}
		autoAddedSamples = append(autoAddedSamples, channelModelSamples(summary.ChannelName, summary.AutoAddedModels)...)
		autoRemovedSamples = append(autoRemovedSamples, channelModelSamples(summary.ChannelName, summary.AutoRemovedModels)...)
		reserveRefusedSamples = append(reserveRefusedSamples, channelModelSamples(summary.ChannelName, summary.ReserveRefusedModels)...)
		if len(summary.RemovalLimitCandidates) > 0 {
			removalLimitChannels = append(removalLimitChannels, fmt.Sprintf("%s (%d)", summary.ChannelName, len(summary.RemovalLimitCandidates)))
		}
	}
	writeModelSamples(&builder, lang, i18n.MsgChannelModelUpdateAutoAdded, autoAddedSamples)
	writeModelSamples(&builder, lang, i18n.MsgChannelModelUpdateAutoRemoved, autoRemovedSamples)
	if len(removalLimitChannels) > 0 {
		builder.WriteString("\n\n" + i18n.Translate(lang, i18n.MsgChannelModelUpdateRemovalLimit, map[string]any{"Limit": domain.MaxAutoRemovedModels, "Channels": strings.Join(removalLimitChannels, ", ")}))
	}
	writeModelSamples(&builder, lang, i18n.MsgChannelModelUpdateReserveRefused, reserveRefusedSamples)
	conflictSamples := make([]string, len(report.ConflictModels))
	for i, conflict := range report.ConflictModels {
		conflictSamples[i] = fmt.Sprintf("%s (%s)", conflict.Model, conflict.ChannelName)
	}
	writeModelSamples(&builder, lang, i18n.MsgChannelModelUpdateConflictSamples, conflictSamples)
	writeModelSamples(&builder, lang, i18n.MsgChannelModelUpdateAutoApplyBlocked, blockedChannels)
	unresolvedMappingChannels := make([]string, len(report.UnresolvedMappingChannels))
	for i, unresolved := range report.UnresolvedMappingChannels {
		unresolvedMappingChannels[i] = fmt.Sprintf("#%d %s", unresolved.ChannelID, unresolved.ChannelName)
	}
	writeModelSamples(&builder, lang, i18n.MsgChannelModelUpdateUnresolvedMapping, unresolvedMappingChannels)

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
