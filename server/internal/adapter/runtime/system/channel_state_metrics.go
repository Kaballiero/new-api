package system

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

const channelStateMetricName = "new_api_channel_state"

type channelStateCollector struct {
	desc       *prometheus.Desc
	infoDesc   *prometheus.Desc
	reasonDesc *prometheus.Desc
	timeDesc   *prometheus.Desc
	query      func() ([]ChannelStateSnapshot, error)
}

func newChannelStateCollector(query func() ([]ChannelStateSnapshot, error)) *channelStateCollector {
	return &channelStateCollector{
		desc: prometheus.NewDesc(
			channelStateMetricName,
			"Current persisted state of an AI channel (1 = current state).",
			[]string{"channel_id", "channel_type", "state"},
			nil,
		),
		infoDesc:   prometheus.NewDesc("new_api_channel_info", "Current channel name and configured provider (1 = current metadata).", []string{"channel_id", "channel_type", "channel_name", "provider"}, nil),
		reasonDesc: prometheus.NewDesc("new_api_channel_state_reason", "Bounded explanation of the current persisted channel state (1 = current explanation).", []string{"channel_id", "channel_type", "state", "reason_category", "upstream_status"}, nil),
		timeDesc:   prometheus.NewDesc("new_api_channel_state_changed_timestamp_seconds", "Persisted channels.other_info.status_time in Unix seconds; omitted when missing or invalid. Legacy paths may retain an earlier timestamp.", []string{"channel_id", "channel_type"}, nil),
		query:      query,
	}
}

func (collector *channelStateCollector) Describe(metrics chan<- *prometheus.Desc) {
	metrics <- collector.desc
	metrics <- collector.infoDesc
	metrics <- collector.reasonDesc
	metrics <- collector.timeDesc
}

func (collector *channelStateCollector) Collect(metrics chan<- prometheus.Metric) {
	snapshots, err := collector.query()
	if err != nil {
		common.SysError(fmt.Sprintf("failed to collect channel state metrics: %v", err))
		metrics <- prometheus.NewInvalidMetric(collector.desc, errors.New("channel state snapshot unavailable"))
		return
	}
	for _, snapshot := range snapshots {
		metrics <- prometheus.MustNewConstMetric(
			collector.desc,
			prometheus.GaugeValue,
			1,
			strconv.Itoa(snapshot.ID),
			strconv.Itoa(snapshot.Type),
			channelStateLabel(snapshot.Status),
		)
		id, channelType := strconv.Itoa(snapshot.ID), strconv.Itoa(snapshot.Type)
		metrics <- prometheus.MustNewConstMetric(collector.infoDesc, prometheus.GaugeValue, 1, id, channelType, snapshot.Name, constant.GetChannelTypeName(snapshot.Type))
		category, upstreamStatus, timestamp := channelStateContext(snapshot)
		metrics <- prometheus.MustNewConstMetric(collector.reasonDesc, prometheus.GaugeValue, 1, id, channelType, channelStateLabel(snapshot.Status), category, upstreamStatus)
		if timestamp > 0 {
			metrics <- prometheus.MustNewConstMetric(collector.timeDesc, prometheus.GaugeValue, float64(timestamp), id, channelType)
		}
	}
}

func channelStateContext(snapshot ChannelStateSnapshot) (category, upstreamStatus string, timestamp int64) {
	var metadata struct {
		Reason json.RawMessage `json:"status_reason"`
		Time   json.RawMessage `json:"status_time"`
	}
	var reason string
	if common.UnmarshalJsonStr(snapshot.OtherInfo, &metadata) == nil {
		_ = common.Unmarshal(metadata.Reason, &reason)
		if common.Unmarshal(metadata.Time, &timestamp) != nil || timestamp < 0 {
			timestamp = 0
		}
	}
	switch snapshot.Status {
	case common.ChannelStatusEnabled:
		return "none", "none", timestamp
	case common.ChannelStatusManuallyDisabled:
		return "manual", "none", timestamp
	case common.ChannelStatusAutoDisabled:
	default:
		return "unknown", "unknown", timestamp
	}
	text := strings.ToLower(strings.TrimSpace(reason))
	upstreamStatus = "unknown"
	code := 0
	for _, prefix := range []string{"status_code=", "bad response status code "} {
		value, ok := strings.CutPrefix(text, prefix)
		if !ok || len(value) < 3 || len(value) > 3 && value[3] != ',' && value[3] != ' ' && value[3] != ':' {
			continue
		}
		parsed, err := strconv.Atoi(value[:3])
		if err == nil && parsed >= 100 && parsed <= 599 {
			code, upstreamStatus = parsed, strconv.Itoa(parsed)
		}
		break
	}
	switch {
	case code == http.StatusPaymentRequired:
		return "credits", upstreamStatus, timestamp
	case code == http.StatusUnauthorized || code == http.StatusForbidden:
		return "auth", upstreamStatus, timestamp
	case code == http.StatusRequestTimeout || code == http.StatusGatewayTimeout:
		return "timeout", upstreamStatus, timestamp
	case code == http.StatusTooManyRequests:
		return "rate_limit", upstreamStatus, timestamp
	case code >= 500:
		return "provider_error", upstreamStatus, timestamp
	}
	for _, known := range []struct{ category, text string }{
		{"credits", "insufficient credits"},
		{"credits", "credit balance is too low"},
		{"credits", "insufficient upstream credits"},
		{"auth", "invalid api key"},
		{"auth", "incorrect api key"},
		{"auth", "invalid_api_key"},
		{"timeout", "context deadline exceeded"},
		{"timeout", "client.timeout exceeded"},
		{"timeout", "i/o timeout"},
		{"timeout", "timeout awaiting response headers"},
		{"rate_limit", "rate limit exceeded"},
		{"rate_limit", "rate_limit_exceeded"},
		{"rate_limit", "too many requests"},
	} {
		if strings.Contains(text, known.text) {
			return known.category, upstreamStatus, timestamp
		}
	}
	return "unknown", upstreamStatus, timestamp
}

func channelStateLabel(status int) string {
	switch status {
	case common.ChannelStatusEnabled:
		return "enabled"
	case common.ChannelStatusManuallyDisabled:
		return "manually_disabled"
	case common.ChannelStatusAutoDisabled:
		return "auto_disabled"
	default:
		return "unknown"
	}
}

func NewChannelStateMetricsHandler(query func() ([]ChannelStateSnapshot, error)) http.Handler {
	registry := prometheus.NewRegistry()
	registry.MustRegister(newChannelStateCollector(query))
	return promhttp.HandlerFor(registry, promhttp.HandlerOpts{
		ErrorHandling: promhttp.HTTPErrorOnError,
	})
}

type ChannelStateSnapshot struct {
	ID        int
	Type      int
	Status    int
	Name      string
	OtherInfo string
}
