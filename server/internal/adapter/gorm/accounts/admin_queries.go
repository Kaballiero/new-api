package accountsadapter

import (
	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/internal/accounts/domain"
	"github.com/QuantumNous/new-api/internal/accounts/list_accounts"
	"github.com/QuantumNous/new-api/internal/accounts/search_accounts"
	core "github.com/QuantumNous/new-api/internal/adapter/gorm/accounts/core"
	accountrecord "github.com/QuantumNous/new-api/internal/adapter/gorm/accounts/record"
)

// AdminQueriesStore связывает административные сценарии с существующими запросами БД.
type AdminQueriesStore struct{ SQL core.Store }

func (store AdminQueriesStore) List(input list_accounts.PageInput, order list_accounts.SortInput) ([]*domain.User, int64, error) {
	users, total, err := store.SQL.GetAllUsers(&common.PageInfo{Page: input.Page, PageSize: input.PageSize}, accountrecord.NewUserSortOptions(order.By, order.Order))
	return adminQueryUsers(users), total, err
}
func (store AdminQueriesStore) Search(input search_accounts.Input) ([]*domain.User, int64, error) {
	users, total, err := store.SQL.SearchUsers(input.Keyword, input.Group, input.Role, input.Status, (input.Page.Page-1)*input.Page.PageSize, input.Page.PageSize, accountrecord.NewUserSortOptions(input.Sort.By, input.Sort.Order))
	return adminQueryUsers(users), total, err
}
func (store AdminQueriesStore) Batch(ids []int) ([]domain.UserBatchRow, error) {
	users, err := store.SQL.GetUsersBatch(ids)
	if users == nil {
		return nil, err
	}
	result := make([]domain.UserBatchRow, len(users))
	for i, user := range users {
		result[i] = domain.UserBatchRow{Id: user.Id, Role: user.Role, Status: user.Status, Quota: user.Quota, UsedQuota: user.UsedQuota, RequestCount: user.RequestCount, Group: user.Group}
		if user.DeletedAt.Valid {
			value := user.DeletedAt.Time
			result[i].DeletedAt = &value
		}
	}
	return result, err
}

func adminQueryUsers(users []*accountrecord.User) []*domain.User {
	if users == nil {
		return nil
	}
	result := make([]*domain.User, len(users))
	for i, user := range users {
		if user == nil {
			continue
		}
		result[i] = adminQueryUser(user)
	}
	return result
}

func adminQueryUser(user *accountrecord.User) *domain.User {
	if user == nil {
		return nil
	}
	value := &domain.User{
		Id:               user.Id,
		Username:         user.Username,
		Password:         user.Password,
		OriginalPassword: user.OriginalPassword,
		DisplayName:      user.DisplayName,
		Role:             user.Role,
		Status:           user.Status,
		Email:            user.Email,
		GitHubId:         user.GitHubId,
		DiscordId:        user.DiscordId,
		OidcId:           user.OidcId,
		WeChatId:         user.WeChatId,
		TelegramId:       user.TelegramId,
		VerificationCode: user.VerificationCode,
		Quota:            user.Quota,
		UsedQuota:        user.UsedQuota,
		RequestCount:     user.RequestCount,
		Group:            user.Group,
		AffCode:          user.AffCode,
		AffCount:         user.AffCount,
		AffQuota:         user.AffQuota,
		AffHistoryQuota:  user.AffHistoryQuota,
		InviterId:        user.InviterId,
		LinuxDOId:        user.LinuxDOId,
		Setting:          user.Setting,
		Remark:           user.Remark,
		StripeCustomer:   user.StripeCustomer,
		CreatedAt:        user.CreatedAt,
		LastLoginAt:      user.LastLoginAt,
		AdminPermissions: user.AdminPermissions,
	}
	if user.DeletedAt.Valid {
		deletedAt := user.DeletedAt.Time
		value.DeletedAt = &deletedAt
	}
	return value
}
