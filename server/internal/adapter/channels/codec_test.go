package channels

import (
	"math"
	"testing"

	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/internal/adapter/gorm/channels/record"
	"github.com/QuantumNous/new-api/internal/channels/domain"
	"github.com/QuantumNous/new-api/internal/channels/update_channel"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestChannelCodecParity(t *testing.T) {
	t.Run("other info diagnostics and retained values", func(t *testing.T) {
		tag := "tag"
		var logs []string
		codec := record.Codec{Log: func(message string) { logs = append(logs, message) }}
		channel := &domain.Channel{Id: 7, Name: "channel", Tag: &tag, OtherInfo: `{"kept":0,"broken":`}
		assert.Empty(t, codec.ReadOtherInfo(channel))
		require.Equal(t, []string{"failed to unmarshal other info: channel_id=7, tag=tag, name=channel, error=unexpected end of JSON input"}, logs)
		channel.OtherInfo = `null`
		assert.Nil(t, codec.ReadOtherInfo(channel))
		codec.WriteOtherInfo(channel, map[string]any{"bad": math.Inf(1)})
		assert.Equal(t, `null`, channel.OtherInfo)
		require.Len(t, logs, 2)
		assert.Equal(t, "failed to marshal other info: channel_id=7, tag=tag, name=channel, error=json: unsupported value: +Inf", logs[1])
		codec.WriteOtherInfo(channel, map[string]any{"status_time": int64(123), "status_reason": "", "kept": false})
		assert.Equal(t, `{"kept":false,"status_reason":"","status_time":123}`, channel.OtherInfo)
		assert.Equal(t, map[string]any{"kept": false, "status_reason": "", "status_time": float64(123)}, codec.ReadOtherInfo(channel))
	})
	t.Run("state decodes metadata only when updating it", func(t *testing.T) {
		var logs []string
		codec := record.Codec{Log: func(message string) { logs = append(logs, message) }}
		channel := &domain.Channel{Id: 7, Status: constant.ChannelStatusEnabled, OtherInfo: `{`, ChannelInfo: domain.ChannelInfo{IsMultiKey: true}}
		now := func() int64 { return 123 }
		domain.ApplyMultiKeyState(channel, []string{"a", "b"}, "a", constant.ChannelStatusAutoDisabled, "failed", now, codec, codec.Log)
		assert.Empty(t, logs)
		assert.Equal(t, `{`, channel.OtherInfo)
		domain.ApplyMultiKeyState(channel, []string{"a", "b"}, "b", constant.ChannelStatusAutoDisabled, "failed", now, codec, codec.Log)
		require.Len(t, logs, 1)
		assert.Equal(t, constant.ChannelStatusAutoDisabled, channel.Status)
		assert.Equal(t, `{"status_reason":"All keys are disabled","status_time":123}`, channel.OtherInfo)
	})
	t.Run("mapping nil empty exact values and partial errors", func(t *testing.T) {
		codec := record.Codec{}
		for _, test := range []struct {
			raw     string
			want    map[string]string
			encoded string
		}{
			{`null`, nil, `null`}, {`{}`, map[string]string{}, `{}`},
			{`{" A ":" B ","zero":""}`, map[string]string{" A ": " B ", "zero": ""}, `{" A ":" B ","zero":""}`},
		} {
			mapping, err := codec.DecodeModelMapping(test.raw)
			require.NoError(t, err)
			assert.Equal(t, test.want, mapping)
			raw, err := codec.EncodeModelMapping(mapping)
			require.NoError(t, err)
			assert.Equal(t, test.encoded, raw)
		}
		mapping, err := codec.DecodeModelMapping(`{"a":"kept","b":4}`)
		require.Error(t, err)
		assert.Equal(t, map[string]string{"a": "kept", "b": ""}, mapping)
	})
	t.Run("key array encodings stay distinct", func(t *testing.T) {
		codec := record.Codec{}
		raw := `[ " key ", { "project_id": "p" }, null, 0, false ]`
		keys, err := codec.KeyArray(raw)
		require.NoError(t, err)
		assert.Equal(t, []string{`" key "`, `{ "project_id": "p" }`, `null`, `0`, `false`}, keys)
		keys, err = domain.VertexArrayKeys(raw, codec.DecodeVertexKeys)
		require.NoError(t, err)
		assert.Equal(t, []string{"key", `{"project_id":"p"}`, `null`, `0`, `false`}, keys)
		_, err = domain.VertexArrayKeys(`[]`, codec.DecodeVertexKeys)
		assert.ErrorIs(t, err, domain.ErrVertexKeysEmpty)
		_, err = domain.VertexArrayKeys(`[`, codec.DecodeVertexKeys)
		assert.ErrorIs(t, err, domain.ErrVertexKeysInvalid)
		assert.Equal(t, []string{"[]"}, codec.ReconciledKeys(`[]`))
		assert.Equal(t, []string{"["}, codec.ReconciledKeys(`[`))
		assert.Empty(t, codec.ReconciledKeys(""))
	})
	t.Run("codex fields and credential bytes", func(t *testing.T) {
		codec := record.Codec{}
		key, err := domain.ParseCodexOAuthKey(`{"access_token":"token","account_id":"account","unknown":true}`, codec.DecodeCodexKey)
		require.NoError(t, err)
		raw, err := codec.EncodeCodexKey(key)
		require.NoError(t, err)
		assert.Equal(t, `{"access_token":"token","account_id":"account"}`, raw)
		_, err = domain.ParseCodexOAuthKey(" ", codec.DecodeCodexKey)
		assert.EqualError(t, err, "codex channel: empty oauth key")
		_, err = domain.ParseCodexOAuthKey(`{"access_token":4}`, codec.DecodeCodexKey)
		assert.EqualError(t, err, "codex channel: invalid oauth key json")
		fields, err := codec.DecodeCodexFields(`{"access_token":4,"account_id":null}`)
		require.NoError(t, err)
		assert.Equal(t, map[string]any{"access_token": float64(4), "account_id": nil}, fields)
	})
	t.Run("patch snapshot and explicit zero overlay", func(t *testing.T) {
		codec := ChannelPatchCodec{}
		channel := &domain.Channel{Keys: []string{"cache-only"}, ChannelInfo: domain.ChannelInfo{MultiKeyStatusList: map[int]int{}, MultiKeyDisabledReason: map[int]string{}}}
		snapshot, err := codec.Snapshot(channel)
		require.NoError(t, err)
		assert.Nil(t, snapshot.Keys)
		assert.NotNil(t, snapshot.ChannelInfo.MultiKeyStatusList)
		assert.Nil(t, snapshot.ChannelInfo.MultiKeyDisabledReason)
		patch := update_channel.PatchChannel{Channel: snapshot}
		require.NoError(t, codec.DecodePatch([]byte(`{"weight":0,"base_url":"","multi_key_mode":"polling","key_mode":"append"}`), &patch))
		require.NotNil(t, patch.Weight)
		assert.Zero(t, *patch.Weight)
		require.NotNil(t, patch.BaseURL)
		assert.Empty(t, *patch.BaseURL)
		require.NotNil(t, patch.MultiKeyMode)
		assert.Equal(t, "polling", *patch.MultiKeyMode)
		require.NotNil(t, patch.KeyMode)
		assert.Equal(t, "append", *patch.KeyMode)
	})
}
