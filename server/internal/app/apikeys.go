package app

import (
	"github.com/QuantumNous/new-api/common"
	tokenhttp "github.com/QuantumNous/new-api/internal/adapter/apikeyhttp"
	repository "github.com/QuantumNous/new-api/internal/adapter/gorm/apikeys"
	record "github.com/QuantumNous/new-api/internal/adapter/gorm/apikeys/record"
	settings "github.com/QuantumNous/new-api/internal/adapter/settings/apikeys"
	"github.com/QuantumNous/new-api/internal/apikeys/create_key"
	"github.com/QuantumNous/new-api/internal/apikeys/delete_key"
	"github.com/QuantumNous/new-api/internal/apikeys/delete_keys_batch"
	"github.com/QuantumNous/new-api/internal/apikeys/domain"
	"github.com/QuantumNous/new-api/internal/apikeys/get_key"
	"github.com/QuantumNous/new-api/internal/apikeys/get_key_groups"
	"github.com/QuantumNous/new-api/internal/apikeys/get_key_status"
	"github.com/QuantumNous/new-api/internal/apikeys/get_key_usage"
	"github.com/QuantumNous/new-api/internal/apikeys/list_keys"
	"github.com/QuantumNous/new-api/internal/apikeys/reveal_key"
	"github.com/QuantumNous/new-api/internal/apikeys/reveal_keys_batch"
	"github.com/QuantumNous/new-api/internal/apikeys/search_keys"
	"github.com/QuantumNous/new-api/internal/apikeys/update_key"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/gin-gonic/gin"
)

type APIKeyEffects struct {
	Audit      func(*gin.Context) map[string]any
	BatchAudit func(*gin.Context, []int) map[string]any
}
type APIKeysHTTP struct {
	ListKeys        list_keys.HTTP
	SearchKeys      search_keys.HTTP
	GetKey          get_key.HTTP
	RevealKey       reveal_key.HTTP
	DeleteKey       delete_key.HTTP
	GetKeyStatus    get_key_status.HTTP
	GetKeyUsage     get_key_usage.HTTP
	CreateKey       create_key.HTTP
	UpdateKey       update_key.HTTP
	DeleteKeysBatch delete_keys_batch.HTTP
	RevealKeysBatch reveal_keys_batch.HTTP
	GetKeyGroups    get_key_groups.HTTP
}

func NewAPIKeysHTTP(effects APIKeyEffects) APIKeysHTTP {
	store := repository.NewStore(model.NewTokenPersistence(), func(key string) (*domain.Token, error) {
		token, err := model.GetTokenByKey(key, false)
		return record.ToDomain((*record.Token)(token)), err
	})
	lookup := func(id int) (string, error) { return model.GetUserGroup(id, false) }
	groups := func(c *gin.Context) domain.AutoGroupRules {
		return domain.AutoGroupRules{Max: setting.GetMaxTokenAutoGroups, UserGroup: func() (string, error) { return tokenhttp.RequestGroup(c, lookup) }, Selectable: service.IsUserSelectableGroup, Encode: store.SetAutoGroups}
	}
	return APIKeysHTTP{
		ListKeys: list_keys.NewHTTP(store), SearchKeys: search_keys.NewHTTP(store, operation_setting.GetMaxUserTokens, common.SysLog, common.SysError), GetKey: get_key.NewHTTP(store), RevealKey: reveal_key.NewHTTP(store, effects.Audit), DeleteKey: delete_key.NewHTTP(store, effects.Audit), GetKeyStatus: get_key_status.NewHTTP(store), GetKeyUsage: get_key_usage.NewHTTP(store),
		CreateKey:       create_key.NewHTTP(store, groups, settings.MaxQuota, operation_setting.GetMaxUserTokens, common.GetTimestamp, common.GenerateKey, effects.Audit),
		UpdateKey:       update_key.NewHTTP(store, groups, settings.MaxQuota, common.GetTimestamp, effects.Audit),
		DeleteKeysBatch: delete_keys_batch.NewHTTP(store, effects.BatchAudit), RevealKeysBatch: reveal_keys_batch.NewHTTP(store, effects.BatchAudit),
		GetKeyGroups: get_key_groups.NewHTTP(func(c *gin.Context) get_key_groups.Reader {
			return tokenhttp.GroupSource{Read: func() (string, error) { return tokenhttp.RequestGroup(c, lookup) }, List: service.GetUserAutoGroup, Limit: setting.GetMaxTokenAutoGroups}
		}),
	}
}
