package domain

type Source int

const (
	Management Source = iota
	Security
	Login
	AdminFallback
	TokenOperation
	AccessToken
	Integration
	QuotaAdjustment
	SubscriptionReset
)

type Recording struct {
	Source                                        Source
	Actor                                         Actor
	TargetID                                      int
	Action, Content, TokenRef                     string
	Parameters                                    map[string]any
	Request                                       RequestFacts
	Response                                      ResponseResult
	Operator                                      *Operator
	EventID                                       string
	CreatedAt                                     int64
	AlreadyLogged                                 bool
	SecurityCode, LoginMethod, VerificationMethod string
	IntegrationID                                 string
	Success                                       bool
}
