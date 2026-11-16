package domain

import (
	"crypto/sha256"
	"encoding/hex"
	"slices"
	"strconv"
	"strings"
)

// ModelUpdateChannelSummary is a channel with pending changes; AutoApplyBlocked
// marks a channel whose upstream models a scheduled run does not apply.
type ModelUpdateChannelSummary struct {
	ChannelID           int
	ChannelName         string
	AddCount            int
	RemoveCount         int
	PendingAddModels    []string
	PendingRemoveModels []string
	AutoApplyBlocked    bool
}

// ModelUpdateReport is the content of the upstream model update notification
// sent after a scan that found changes or failures.
type ModelUpdateReport struct {
	CheckedChannels      int
	ChangedChannels      int
	DetectedAddModels    int
	DetectedRemoveModels int
	AutoAddedModels      int
	FailedChannelIDs     []int
	Channels             []ModelUpdateChannelSummary
	AddModelSamples      []string
	RemoveModelSamples   []string
	ConflictModels       []ModelUpdateConflict
}

// ModelUpdateConflict is an upstream model of a channel that was not offered
// for addition because its canonical name is taken by another target.
type ModelUpdateConflict struct {
	ChannelID   int
	ChannelName string
	Model       string
}

type ModelUpdateSummary struct {
	CheckedChannels      int `json:"checked_channels"`
	ChangedChannels      int `json:"changed_channels"`
	DetectedAddModels    int `json:"detected_add_models"`
	DetectedRemoveModels int `json:"detected_remove_models"`
	FailedChannels       int `json:"failed_channels"`
	AutoAddedModels      int `json:"auto_added_models"`
}

// ModelUpdateReportFingerprint identifies the decisions a report asks for:
// the pending additions, removals and conflicts of each channel, whether its
// changes are blocked from auto-apply, and the failed channels, in any order.
// Applied changes are not part of it: the run that applies a change reports it
// once.
func ModelUpdateReportFingerprint(report ModelUpdateReport) string {
	var lines []string
	for _, channel := range report.Channels {
		channelID := strconv.Itoa(channel.ChannelID)
		for _, model := range channel.PendingAddModels {
			lines = append(lines, channelID+" add "+model)
		}
		for _, model := range channel.PendingRemoveModels {
			lines = append(lines, channelID+" remove "+model)
		}
		if channel.AutoApplyBlocked {
			lines = append(lines, channelID+" auto_apply_blocked")
		}
	}
	for _, conflict := range report.ConflictModels {
		lines = append(lines, strconv.Itoa(conflict.ChannelID)+" conflict "+conflict.Model)
	}
	for _, channelID := range report.FailedChannelIDs {
		lines = append(lines, strconv.Itoa(channelID)+" failed")
	}
	slices.Sort(lines)
	fingerprint := sha256.Sum256([]byte(strings.Join(lines, "\n")))
	return hex.EncodeToString(fingerprint[:])
}
