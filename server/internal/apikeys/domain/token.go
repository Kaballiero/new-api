package domain

import (
	"slices"
	"strings"
	"time"
)

type Token struct {
	CacheSchema        int
	Id                 int
	UserId             int
	Key                string
	Status             int
	Name               string
	CreatedTime        int64
	AccessedTime       int64
	ExpiredTime        int64
	RemainQuota        int
	BatchHeldQuota     int64
	BatchQuotaVersion  int64
	UnlimitedQuota     bool
	ModelLimitsEnabled bool
	ModelLimits        string
	AllowIps           *string
	UsedQuota          int
	Group              string
	CrossGroupRetry    bool
	AutoGroups         string
	DeletedAt          *time.Time
}

const (
	StatusEnabled   = 1
	StatusDisabled  = 2
	StatusExpired   = 3
	StatusExhausted = 4
)

func MaskKey(key string) string {
	if key == "" {
		return ""
	}
	if len(key) <= 4 {
		return strings.Repeat("*", len(key))
	}
	if len(key) <= 8 {
		return key[:2] + "****" + key[len(key)-2:]
	}
	return key[:4] + "**********" + key[len(key)-4:]
}

// NameTaken reports whether name equals one of names, ignoring surrounding
// whitespace and Unicode case. A key name is unique among the keys of its user
// that are not deleted.
func NameTaken(names []string, name string) bool {
	name = strings.TrimSpace(name)
	return slices.ContainsFunc(names, func(other string) bool { return strings.EqualFold(strings.TrimSpace(other), name) })
}

func (t *Token) GetModelLimits() []string {
	if t.ModelLimits == "" {
		return []string{}
	}
	return strings.Split(t.ModelLimits, ",")
}

func (t *Token) GetModelLimitsMap() map[string]bool {
	result := make(map[string]bool)
	for _, limit := range t.GetModelLimits() {
		result[limit] = true
	}
	return result
}

func (t *Token) GetIpLimits() []string {
	ipLimits := make([]string, 0)
	if t.AllowIps == nil {
		return ipLimits
	}
	cleanIps := strings.ReplaceAll(*t.AllowIps, " ", "")
	if cleanIps == "" {
		return ipLimits
	}
	for ip := range strings.SplitSeq(cleanIps, "\n") {
		ip = strings.TrimSpace(ip)
		ip = strings.ReplaceAll(ip, ",", "")
		if ip != "" {
			ipLimits = append(ipLimits, ip)
		}
	}
	return ipLimits
}
