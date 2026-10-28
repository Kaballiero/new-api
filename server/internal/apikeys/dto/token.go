package dto

import (
	"fmt"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/internal/apikeys/domain"
)

type Token struct {
	CacheSchema        int     `json:"-"`
	Id                 int     `json:"id"`
	UserId             int     `json:"user_id"`
	Key                string  `json:"key"`
	Status             int     `json:"status"`
	Name               string  `json:"name"`
	CreatedTime        int64   `json:"created_time"`
	AccessedTime       int64   `json:"accessed_time"`
	ExpiredTime        int64   `json:"expired_time"` // -1 means never expired
	RemainQuota        int     `json:"remain_quota"`
	BatchHeldQuota     int64   `json:"-"`
	BatchQuotaVersion  int64   `json:"-"`
	UnlimitedQuota     bool    `json:"unlimited_quota"`
	ModelLimitsEnabled bool    `json:"model_limits_enabled"`
	ModelLimits        string  `json:"model_limits"`
	AllowIps           *string `json:"allow_ips"`
	UsedQuota          int     `json:"used_quota"` // used quota
	Group              string  `json:"group"`
	CrossGroupRetry    bool    `json:"cross_group_retry"` // cross-group retry, only for the auto group
	AutoGroups         string  `json:"-"`
	DeletedAt          *time.Time
}

type AutoGroupsInput struct {
	Set    bool
	Groups []string
}

func (input *AutoGroupsInput) UnmarshalJSON(data []byte) error {
	input.Set = true
	if strings.TrimSpace(string(data)) == "null" {
		input.Groups = nil
		return nil
	}
	return common.Unmarshal(data, &input.Groups)
}

type Request struct {
	Token
	AutoGroups AutoGroupsInput `json:"auto_groups"`
}
type Response struct {
	*Token
	AutoGroups []string `json:"auto_groups"`
}

func (t Token) Domain() domain.Token { return domain.Token(t) }
func Masked(t *domain.Token, mask func(string) string, log func(string)) *Response {
	if t == nil {
		return nil
	}
	wire := Token(*t)
	wire.Key = mask(t.Key)
	var groups []string
	if t.AutoGroups != "" {
		if err := common.UnmarshalJsonStr(t.AutoGroups, &groups); err != nil {
			log(fmt.Sprintf("failed to parse auto groups for token %d: %v", t.Id, err))
			groups = nil
		}
	}
	if len(groups) == 0 {
		groups = nil
	}
	return &Response{Token: &wire, AutoGroups: groups}
}
func MaskedList(tokens []*domain.Token, mask func(string) string, log func(string)) []*Response {
	result := make([]*Response, 0, len(tokens))
	for _, t := range tokens {
		result = append(result, Masked(t, mask, log))
	}
	return result
}
