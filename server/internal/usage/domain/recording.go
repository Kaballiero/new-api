package domain

import (
	"strings"

	billingdomain "github.com/QuantumNous/new-api/internal/billing/domain"
)

func DeliveryFailure(requestError string, stream *Stream) string {
	if requestError != "" {
		return requestError
	}
	if stream == nil {
		return ""
	}
	if !stream.Normal {
		if stream.EndReason == "" {
			return "stream_incomplete"
		}
		return "stream_" + stream.EndReason
	}
	if stream.ErrorCount > 0 {
		return "stream_error"
	}
	return ""
}
func UsagePath(local, canonical, estimated bool, semantic string) string {
	if !canonical {
		if local {
			return "local"
		}
		return "upstream"
	}
	switch semantic {
	case "openai", "anthropic", "gemini":
		value := "billing-usage-" + semantic
		if estimated {
			value += "-estimated"
		}
		return value
	}
	return "upstream"
}
func LogModel(model string) string {
	if strings.HasPrefix(model, "gpt-4-gizmo") {
		return "gpt-4-gizmo-*"
	}
	if strings.HasPrefix(model, "gpt-4o-gizmo") {
		return "gpt-4o-gizmo-*"
	}
	return model
}

// SubscriptionUsage projects the request's settled allocation for log readers.
func SubscriptionUsage(in RequestFacts) (consumed, used, remaining int64) {
	consumed = max(in.SubscriptionPreConsumed+in.SubscriptionPostDelta, 0)
	used = max(in.SubscriptionAmountUsedAfterPreConsume+in.SubscriptionPostDelta, 0)
	remaining = max(in.SubscriptionAmountTotal-used, 0)
	return
}

// Notes describe why a request's consumption differs from the ordinary path.
// Rendering and persisted punctuation belong to the serializer.
type NoteKind int

const (
	MissingUpstreamUsage NoteKind = iota
	ToolCharge
	AudioInputCharge
	UnbillableUsage
	SettlementRepair
	OriginalModel
	AudioRatios
	ModelPrice
	PossibleTimeout
	DeliveryFailed
	LiteralContent
)

type NoteJoin int

const (
	CommaSeparated NoteJoin = iota
	Adjacent
)

type Note struct {
	Join                             NoteJoin
	Kind                             NoteKind
	Name                             string
	Count, Quota                     int
	Prices                           Prices
	AudioRatio, AudioCompletionRatio float64
}
type TextDescription struct {
	OriginMissing, Billable bool
	Tools                   []billingdomain.ToolSurchargeItem
	AudioPrice              float64
	AudioTokens, AudioQuota int
}

func TextNotes(in TextDescription) []Note {
	var notes []Note
	if in.OriginMissing {
		notes = append(notes, Note{Kind: MissingUpstreamUsage})
	}
	for _, tool := range in.Tools {
		notes = append(notes, Note{
			Kind: ToolCharge, Name: tool.Name, Count: tool.Count, Quota: tool.DisplayQuota,
		})
	}
	if in.AudioPrice > 0 && in.AudioTokens > 0 {
		notes = append(notes, Note{Kind: AudioInputCharge, Quota: in.AudioQuota})
	}
	if !in.Billable {
		notes = append(notes, Note{Kind: UnbillableUsage})
	}
	return notes
}
func FinalTextNotes(model string, outcome *billingdomain.Outcome) (string, []Note) {
	var notes []Note
	if outcome != nil && outcome.SettlementStatus != "settled" {
		notes = append(notes, Note{Kind: SettlementRepair})
	}
	logModel := LogModel(model)
	if logModel != model {
		notes = append(notes, Note{Kind: OriginalModel, Name: model})
	}
	return logModel, notes
}

type AudioDescription struct {
	Prices                           Prices
	AudioRatio, AudioCompletionRatio float64
	UsePrice, Billable               bool
}

func AudioNotes(in AudioDescription) []Note {
	kind := AudioRatios
	if in.UsePrice {
		kind = ModelPrice
	}
	notes := []Note{{
		Kind:                 kind,
		Prices:               in.Prices,
		AudioRatio:           in.AudioRatio,
		AudioCompletionRatio: in.AudioCompletionRatio,
	}}
	if !in.Billable {
		notes = append(notes, Note{Kind: PossibleTimeout, Join: Adjacent})
	}
	return notes
}
