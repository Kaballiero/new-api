package domain

import "context"

type AdmissionFacts struct {
	UserID, TokenID, ChannelID, ChannelType, CredentialIndex int
	UpstreamModel, UsingGroup, UserGroup, TokenGroup         string
	Headers                                                  map[string]string
}
type Admission interface {
	ValidateHeaders() error
	Initialize(CreateRequest) (AdmissionFacts, error)
	Provider(CreateRequest) ([]byte, error)
	PrepareBody(RequestItem) ([]byte, error)
	Count(string, []byte) (int, error)
	Estimate([]byte, []byte) (float64, error)
	ConfigHash() (string, error)
	CredentialHash() string
}
type AdmissionGateway interface {
	Open(context.Context) (Admission, error)
}
