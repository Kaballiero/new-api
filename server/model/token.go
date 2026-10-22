package model

import (
	"errors"

	"github.com/QuantumNous/new-api/common"
	repository "github.com/QuantumNous/new-api/internal/adapter/gorm/apikeys"
	record "github.com/QuantumNous/new-api/internal/adapter/gorm/apikeys/record"
	"github.com/QuantumNous/new-api/internal/apikeys/delete_owned_key"
	"github.com/QuantumNous/new-api/internal/apikeys/disable_model_limits"
	keydomain "github.com/QuantumNous/new-api/internal/apikeys/domain"
	"github.com/QuantumNous/new-api/internal/apikeys/search_keys"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/bytedance/gopkg/util/gopool"
	"gorm.io/gorm"
)

type Token record.Token

func (token *Token) GetAutoGroups() ([]string, error) {
	if token.AutoGroups == "" {
		return nil, nil
	}
	var groups []string
	if err := common.UnmarshalJsonStr(token.AutoGroups, &groups); err != nil {
		return nil, err
	}
	return groups, nil
}

func (token *Token) SetAutoGroups(groups []string) error {
	if len(groups) == 0 {
		token.AutoGroups = ""
		return nil
	}
	data, err := common.Marshal(groups)
	if err != nil {
		return err
	}
	token.AutoGroups = string(data)
	return nil
}

func (token *Token) Clean() {
	token.Key = ""
}

func MaskTokenKey(key string) string { return keydomain.MaskKey(key) }

func (token *Token) GetFullKey() string {
	return token.Key
}

func (token *Token) GetMaskedKey() string {
	return MaskTokenKey(token.Key)
}

func (token *Token) GetIpLimits() []string {
	return record.ToDomain((*record.Token)(token)).GetIpLimits()
}

func GetAllUserTokens(userId, startIdx, num int) ([]*Token, error) {
	ts, err := NewTokenPersistence().List(userId, startIdx, num)
	if ts == nil {
		return nil, err
	}
	out := make([]*Token, len(ts))
	for i, t := range ts {
		out[i] = (*Token)(t)
	}
	return out, err
}

// sanitizeLikePattern validates and escapes a user LIKE search pattern.
// Rules:
//  1. ! and _ are escaped (! is the ESCAPE character, compatible with MySQL/PostgreSQL/SQLite)
//  2. consecutive % are merged into a single %
//  3. at most 2 % are allowed
//  4. with % (fuzzy search) the keyword without % must be at least 2 characters long
//  5. without % the match is exact
func sanitizeLikePattern(input string) (string, error) { return keydomain.SanitizeSearchPattern(input) }
func validateLikePattern(input string) error           { return keydomain.ValidateSearchPattern(input) }
func SearchUserTokens(userId int, keyword, token string, offset, limit int) ([]*Token, int64, error) {
	result, err := search_keys.NewUsecase(NewTokenPersistence(), operation_setting.GetMaxUserTokens, common.SysLog, common.SysError).Execute(search_keys.Input{UserID: userId, Keyword: keyword, Key: token, Start: offset, Limit: limit})
	if err != nil {
		return nil, 0, err
	}
	if result.Tokens == nil {
		return nil, result.Total, nil
	}
	out := make([]*Token, len(result.Tokens))
	for i, t := range result.Tokens {
		out[i] = (*Token)(record.FromDomain(t))
	}
	return out, result.Total, nil
}

func GetTokenByIds(id, userId int) (*Token, error) {
	t, err := NewTokenPersistence().Get(id, userId)
	return (*Token)(t), err
}

func GetTokenById(id int) (*Token, error) {
	t, err := NewTokenPersistence().GetByID(id)
	return (*Token)(t), err
}

// BRIDGE: billing, batch quota and the credential adapter share this cached read.
func GetTokenByKey(key string, fromDB bool) (*Token, error) {
	cache := record.KeyCache{
		Enabled: func() bool { return common.RedisEnabled },
		Get: func(key string) (*record.Token, error) {
			token, err := cacheGetTokenByKey(key)
			return (*record.Token)(token), err
		},
		Initialize: func(token record.Token) (int, error) { return cacheInitToken(Token(token)) },
	}
	token, err := record.NewKeyReader(func() *gorm.DB { return DB }, func() string { return commonKeyCol }, lockForUpdate, cache, common.SysLog).Read(key, fromDB)
	return (*Token)(token), err
}

func (token *Token) Insert() error { return NewTokenPersistence().Insert((*record.Token)(token)) }

// Update Make sure your token's fields is completed, because this will update non-zero values
func (token *Token) Update() error { return NewTokenPersistence().Update((*record.Token)(token)) }

func (token *Token) Delete() error { return NewTokenPersistence().Delete((*record.Token)(token)) }

func (token *Token) IsModelLimitsEnabled() bool {
	return token.ModelLimitsEnabled
}

func (token *Token) GetModelLimits() []string {
	return record.ToDomain((*record.Token)(token)).GetModelLimits()
}
func (token *Token) GetModelLimitsMap() map[string]bool {
	return record.ToDomain((*record.Token)(token)).GetModelLimitsMap()
}
func DisableModelLimits(tokenId int) error {
	return disable_model_limits.NewUsecase(repository.NewStore(NewTokenPersistence(), tokenDomainByKey)).Execute(tokenId)
}
func DeleteTokenById(id, userId int) error {
	return delete_owned_key.NewUsecase(repository.NewStore(NewTokenPersistence(), tokenDomainByKey)).Execute(id, userId)
}

// tokenDomainByKey adapts the shared cached read for API key scenarios.
func tokenDomainByKey(key string) (*keydomain.Token, error) {
	token, err := GetTokenByKey(key, false)
	return record.ToDomain((*record.Token)(token)), err
}

func IncreaseTokenQuota(tokenId int, key string, quota int) (err error) {
	if quota < 0 {
		return errors.New("quota must not be negative")
	}
	if common.RedisEnabled {
		gopool.Go(func() {
			// Guarded increment: a missing hash is skipped and hydrated from the
			// database by the next read; a partial quota-only hash is never created.
			if _, err := cacheApplyTokenQuotaDelta(tokenId, key, int64(quota)); err != nil {
				common.SysLog("failed to increase token quota: " + err.Error())
			}
		})
	}
	if common.BatchUpdateEnabled {
		addNewRecord(BatchUpdateTypeTokenQuota, tokenId, quota)
		return nil
	}
	return increaseTokenQuota(tokenId, quota)
}

func increaseTokenQuota(id int, quota int) (err error) {
	err = DB.Model(&Token{}).Where("id = ?", id).Updates(
		map[string]any{
			"remain_quota":  gorm.Expr("remain_quota + ?", quota),
			"used_quota":    gorm.Expr("used_quota - ?", quota),
			"accessed_time": common.GetTimestamp(),
		},
	).Error
	return err
}

func DecreaseTokenQuota(id int, key string, quota int) (err error) {
	if quota < 0 {
		return errors.New("quota must not be negative")
	}
	if common.RedisEnabled {
		gopool.Go(func() {
			if _, err := cacheApplyTokenQuotaDelta(id, key, int64(-quota)); err != nil {
				common.SysLog("failed to decrease token quota: " + err.Error())
			}
		})
	}
	if common.BatchUpdateEnabled {
		addNewRecord(BatchUpdateTypeTokenQuota, id, -quota)
		return nil
	}
	return decreaseTokenQuota(id, quota)
}

func decreaseTokenQuota(id int, quota int) (err error) {
	err = DB.Model(&Token{}).Where("id = ?", id).Updates(
		map[string]any{
			"remain_quota":  gorm.Expr("remain_quota - ?", quota),
			"used_quota":    gorm.Expr("used_quota + ?", quota),
			"accessed_time": common.GetTimestamp(),
		},
	).Error
	return err
}

// CountUserTokens returns total number of tokens for the given user, used for pagination
func CountUserTokens(userId int) (int64, error) { return NewTokenPersistence().Count(userId) }

// BatchDeleteTokens deletes a set of tokens of the user and returns the number deleted
func BatchDeleteTokens(ids []int, userId int) (int, error) {
	return NewTokenPersistence().DeleteBatch(ids, userId)
}

func GetTokenKeysByIds(ids []int, userId int) ([]Token, error) {
	ts, err := NewTokenPersistence().KeysBatch(ids, userId)
	if ts == nil {
		return nil, err
	}
	out := make([]Token, len(ts))
	for i, t := range ts {
		out[i] = Token(t)
	}
	return out, err
}

// InvalidateUserTokensCache clears the Redis cache of all tokens of the user.
// Together with InvalidateUserCache it blocks token requests immediately after
// the user is disabled or deleted: the next request reloads the token and the
// user state from the database and sees the disabled user.
func InvalidateUserTokensCache(userId int) error {
	if !common.RedisEnabled {
		return nil
	}
	if userId <= 0 {
		return errors.New("invalid user id")
	}
	var tokens []Token
	if err := DB.Unscoped().
		Select("id", commonKeyCol).
		Where("user_id = ?", userId).
		Find(&tokens).Error; err != nil {
		return err
	}
	return invalidateTokensCache(tokens)
}

func invalidateTokensCache(tokens []Token) error {
	if !common.RedisEnabled {
		return nil
	}
	var firstErr error
	for _, t := range tokens {
		if t.Key == "" {
			continue
		}
		if err := invalidateTokenCacheForMutation(t.Key); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}

// BRIDGE: API key adapters and legacy token mutations share cache fence hooks.
func NewTokenPersistence() record.Store {
	return record.NewStore(func() *gorm.DB { return DB }, func() string { return commonKeyCol }, invalidateTokenCacheForMutation, func(tokens []record.Token) error {
		values := make([]Token, len(tokens))
		for i, t := range tokens {
			values[i] = Token(t)
		}
		return invalidateTokensCache(values)
	})
}
