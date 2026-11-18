package tasksync

import (
	"crypto/sha256"
	"errors"
	"fmt"
	"net/http"
	"reflect"
	"slices"
	"strings"

	"github.com/QuantumNous/new-api/constant"
	channelstore "github.com/QuantumNous/new-api/internal/adapter/gorm/channels"
	"github.com/QuantumNous/new-api/internal/adapter/gorm/tasks/record"
	"github.com/QuantumNous/new-api/internal/adapter/http/batchaccount"
	"github.com/QuantumNous/new-api/internal/tasks/domain"
	"github.com/QuantumNous/new-api/model"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
)

type Accounts struct{}

func (Accounts) Prepare(task *domain.Task, input domain.SyncSubmissionAccount) error {
	task.PrivateData.Key = ""
	channel, err := channelstore.GetChannelById(task.ChannelId, true)
	if err != nil {
		return err
	}
	if channel.Type != input.ChannelType || !reflect.DeepEqual(channelstore.NewChannelPersistence().GetSetting(channel), record.FromDomainTransport(input.Transport)) ||
		(len(input.Headers) > 0 || len(channel.GetHeaderOverride()) > 0) && !reflect.DeepEqual(input.Headers, channel.GetHeaderOverride()) {
		return errors.New("OpenRouter synchronous task channel configuration changed")
	}
	state := task.PrivateData.OpenRouterSyncState()
	state.CredentialHash = fmt.Sprintf("%x", sha256.Sum256([]byte(input.APIKey)))
	state.CredentialIndex = input.CredentialIndex
	state.RoutingHash = input.RoutingHash
	state.ConfigHash, err = batchaccount.BatchChannelConfigHash(channel)
	if err != nil {
		return err
	}
	_, _, err = boundAccount(task)
	return err
}

// Bind records the account binding of a Task stored before binding existed,
// from the account key the Task still holds. The key must still belong to the
// Task's channel.
func (Accounts) Bind(task *domain.Task, key string) error {
	channel, err := channelstore.GetChannelById(task.ChannelId, true)
	if err != nil {
		return err
	}
	keys := []string{channel.Key}
	if channel.ChannelInfo.IsMultiKey {
		keys = channel.GetKeys()
	}
	index := slices.Index(keys, key)
	if index < 0 {
		return errors.New("OpenRouter synchronous task key no longer belongs to its channel")
	}
	state := task.PrivateData.OpenRouterSyncState()
	state.CredentialHash = fmt.Sprintf("%x", sha256.Sum256([]byte(key)))
	state.CredentialIndex = index
	state.ConfigHash, err = batchaccount.BatchChannelConfigHash(channel)
	if err != nil {
		return err
	}
	_, _, err = boundAccount(task)
	return err
}

// ValidateMetadata retains the originally selected account and exact static headers.
func (Accounts) ValidateMetadata(task *domain.Task, metadataURL string, headers map[string][]string) error {
	channel, key, err := boundAccount(task)
	if err != nil {
		return err
	}
	base := channel.GetBaseURL()
	if base == "" {
		base = constant.GetChannelBaseURL(channel.Type)
	}
	if metadataURL != relaycommon.GetFullRequestURL(base, "/v1/generation", channel.Type) {
		return errors.New("OpenRouter synchronous task metadata origin changed")
	}
	requestHeaders := http.Header(headers)
	auth, host := "Bearer "+key, ""
	for name, value := range channel.GetHeaderOverride() {
		if requestHeaders.Get(name) != strings.ReplaceAll(value.(string), "{api_key}", key) {
			return errors.New("OpenRouter synchronous task request headers changed")
		}
		if strings.EqualFold(name, "Authorization") {
			auth = strings.ReplaceAll(value.(string), "{api_key}", key)
		}
		if strings.EqualFold(name, "Host") {
			host = strings.ReplaceAll(value.(string), "{api_key}", key)
		}
	}
	if requestHeaders.Get("Authorization") != auth || requestHeaders.Get("Host") != host {
		return errors.New("OpenRouter synchronous task request account changed")
	}
	return nil
}

// boundAccount resolves only the originally selected account and transport.
// Rotation/config drift blocks lookup instead of switching accounts.
func boundAccount(task *domain.Task) (*model.Channel, string, error) {
	a := task.PrivateData.OpenRouterSyncState()
	channel, err := channelstore.GetChannelById(task.ChannelId, true)
	if err != nil {
		return nil, "", errors.New("synchronous task account unavailable")
	}
	hash, err := batchaccount.BatchChannelConfigHash(channel)
	if err != nil || channel.Type != constant.ChannelTypeOpenRouter || hash != a.ConfigHash {
		return nil, "", errors.New("synchronous task account configuration changed")
	}
	if err := batchaccount.ValidateBatchChannelHeaders(channel); err != nil {
		return nil, "", errors.New("synchronous task recovery requires static account headers")
	}
	key := channel.Key
	if channel.ChannelInfo.IsMultiKey {
		keys := channel.GetKeys()
		if a.CredentialIndex < 0 || a.CredentialIndex >= len(keys) {
			return nil, "", errors.New("synchronous task credential unavailable")
		}
		key = keys[a.CredentialIndex]
	}
	if a.CredentialHash == "" || fmt.Sprintf("%x", sha256.Sum256([]byte(key))) != a.CredentialHash {
		return nil, "", errors.New("synchronous task credential changed")
	}
	base := channel.GetBaseURL()
	if base == "" {
		base = constant.GetChannelBaseURL(channel.Type)
	}
	if a.MetadataURL != "" && a.MetadataURL != relaycommon.GetFullRequestURL(base, "/v1/generation", channel.Type) {
		return nil, "", errors.New("synchronous task metadata origin changed")
	}
	return channel, key, nil
}
