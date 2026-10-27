package login

import (
	"github.com/QuantumNous/new-api/common"
	accountrecord "github.com/QuantumNous/new-api/internal/adapter/gorm/accounts/record"
	identitystore "github.com/QuantumNous/new-api/internal/adapter/gorm/identity"
	identityrecord "github.com/QuantumNous/new-api/internal/adapter/gorm/identity/record"
	identitydomain "github.com/QuantumNous/new-api/internal/identity/domain"
	"github.com/QuantumNous/new-api/internal/identity/dto"

	"time"
)

type Dependencies struct {
	SQL identitystore.Store

	TTL                     time.Duration
	PolicyFunc              func(string, identitystore.UserVerificationState) ([]dto.VerificationMethodOption, error)
	RequireMethodFunc       func(*identitystore.UserVerificationState, string) error
	RequireVerificationFunc func(string, string) (*LoginVerification, error)
	VerifyFactorFunc        func(*identityrecord.TwoFA, string) error
	CompleteFunc            func(string, *LoginVerification, string, string, string) (*dto.AuthBundle, error)
	NewSessionFunc          func(int, int64, string, string, string) (*identityrecord.UserSession, string, error)
	IssueFunc               func(*identityrecord.UserSession, string, bool) (*dto.AuthBundle, error)
}
type loginFlowPayload struct {
	AuthVersion int64  `json:"auth_version"`
	LoginMethod string `json:"login_method"`
}

// LoginVerification is server-owned state read from a primary-authenticated flow.
// It is never constructed from a user ID or an authentication claim in a request.
type LoginVerification struct {
	Flow    *identityrecord.AuthFlow
	State   *identitystore.UserVerificationState
	payload loginFlowPayload
}

type startLoginVerificationOperation struct {
	SQL identitystore.Store

	Dependencies
	primary     *accountrecord.User
	loginMethod string
	state       *identitystore.UserVerificationState
	methods     []dto.VerificationMethodOption
	challenge   *dto.LoginChallenge
}

func (op *startLoginVerificationOperation) PrimaryFacts() (int, int64, string) {
	if op.primary == nil {
		return 0, 0, op.loginMethod
	}
	return op.primary.Id, op.primary.AuthVersion, op.loginMethod
}

func (op *startLoginVerificationOperation) LoadState() (int, int64, error) {
	var err error
	op.state, err = op.SQL.GetUserVerificationState(op.primary.Id)
	if err != nil {
		return 0, 0, err
	}
	return op.state.Status, op.state.AuthVersion, nil
}

func (op *startLoginVerificationOperation) Policy(scope string) (int, bool, error) {
	var err error
	op.methods, err = op.PolicyFunc(scope, *op.state)
	available := false
	for _, method := range op.methods {
		available = available || method.Available
	}
	return len(op.methods), available, err
}

func (op *startLoginVerificationOperation) CreateChallenge() error {
	payload, err := common.Marshal(loginFlowPayload{AuthVersion: op.state.AuthVersion, LoginMethod: op.loginMethod})
	if err != nil {
		return err
	}
	expiresAt := time.Now().Add(op.TTL)
	token, _, err := op.SQL.CreateAuthFlow(identitystore.AuthFlowCreate{Purpose: identitydomain.AuthFlowPurposeLoginVerification, UserId: op.primary.Id, Payload: string(payload), ExpiresAt: expiresAt})
	if err != nil {
		return err
	}
	op.challenge = &dto.LoginChallenge{RequireVerification: true, FlowToken: token, ExpiresAt: expiresAt.Unix(), Methods: op.methods}
	return nil
}

type requireLoginVerificationOperation struct {
	SQL identitystore.Store

	Dependencies
	token   string
	flow    *identityrecord.AuthFlow
	state   *identitystore.UserVerificationState
	payload loginFlowPayload
}

func (op *requireLoginVerificationOperation) LoadFlow() error {
	var err error
	op.flow, err = op.SQL.GetAuthFlow(op.token, identitystore.AuthFlowMatch{Purpose: identitydomain.AuthFlowPurposeLoginVerification})
	return err
}

func (op *requireLoginVerificationOperation) DecodePayload() (int64, string, error) {
	err := common.UnmarshalJsonStr(op.flow.Payload, &op.payload)
	return op.payload.AuthVersion, op.payload.LoginMethod, err
}

func (op *requireLoginVerificationOperation) LoadState() (int, int64, error) {
	var err error
	op.state, err = op.SQL.GetUserVerificationState(op.flow.UserId)
	if err != nil {
		return 0, 0, err
	}
	return op.state.Status, op.state.AuthVersion, nil
}

func (op *requireLoginVerificationOperation) RequireMethod(method string) error {
	return op.RequireMethodFunc(op.state, method)
}

type verifyLoginCodeOperation struct {
	SQL identitystore.Store

	Dependencies
	token, code, ip, userAgent string
	verification               *LoginVerification
	factor                     *identityrecord.TwoFA
	bundle                     *dto.AuthBundle
}

func (op *verifyLoginCodeOperation) RequireVerification() error {
	var err error
	op.verification, err = op.RequireVerificationFunc(op.token, "2fa")
	return err
}

func (op *verifyLoginCodeOperation) LoadFactor() error {
	var err error
	op.factor, err = op.SQL.GetTwoFAByUserId(op.verification.State.UserID)
	return err
}

func (op *verifyLoginCodeOperation) VerifyFactor() error {
	return op.VerifyFactorFunc(op.factor, op.code)
}

func (op *verifyLoginCodeOperation) Complete() error {
	var err error
	op.bundle, err = op.CompleteFunc(op.token, op.verification, "2fa", op.ip, op.userAgent)
	return err
}

type completeLoginVerificationOperation struct {
	SQL identitystore.Store

	Dependencies
	loginCompletion
	verification                 *LoginVerification
	ip, userAgent, refreshSecret string
	bundle                       *dto.AuthBundle
}

func (op *completeLoginVerificationOperation) VerificationValid() bool {
	return op.verification != nil && op.verification.Flow != nil && op.verification.State != nil
}

func (op *completeLoginVerificationOperation) NewSession() error {
	var err error
	op.Session, op.refreshSecret, err = op.NewSessionFunc(op.verification.State.UserID, op.verification.payload.AuthVersion, op.verification.payload.LoginMethod, op.ip, op.userAgent)
	return err
}

func (op *completeLoginVerificationOperation) ExpectedFlowID() int64 { return op.verification.Flow.Id }

func (op *completeLoginVerificationOperation) payloadMatches(flow *identityrecord.AuthFlow) bool {
	// Keep the original early return: another flow never reaches the decoder.
	if flow.Id != op.verification.Flow.Id {
		return false
	}
	var payload loginFlowPayload
	return common.UnmarshalJsonStr(flow.Payload, &payload) == nil && payload == op.verification.payload
}

func (op *completeLoginVerificationOperation) RequireCurrentMethod(method string) error {
	return op.RequireMethodFunc(op.CurrentState, method)
}

func (op *completeLoginVerificationOperation) IssueBundle() error {
	var err error
	op.bundle, err = op.IssueFunc(op.Session, op.Session.SID+"."+op.refreshSecret, true)
	return err
}

func (op *completeLoginVerificationOperation) RevokeCreated(reason string) error {
	_, err := op.SQL.RevokeUserSession(op.Session.UserID, op.Session.SID, reason)
	return err
}

type loginCompletion struct {
	SQL identitystore.Store

	Token          string
	Session        *identityrecord.UserSession
	CurrentState   *identitystore.UserVerificationState
	PayloadMatches func(*identityrecord.AuthFlow) bool
}

func (op *loginCompletion) ConsumeAndCreate(check func(int64, bool) error) error {
	if op.PayloadMatches == nil || check == nil {
		return identitydomain.ErrSessionInvalid
	}
	return op.SQL.CreateUserSessionFromLoginFlow(op.Token, op.Session, func(flow *identityrecord.AuthFlow, state *identitystore.UserVerificationState) error {
		op.CurrentState = state
		return check(flow.Id, op.PayloadMatches(flow))
	})
}

func NewStart(user *accountrecord.User, method string, deps Dependencies) *startLoginVerificationOperation {
	return &startLoginVerificationOperation{SQL: deps.SQL, Dependencies: deps, primary: user, loginMethod: method}
}
func (op *startLoginVerificationOperation) Challenge() *dto.LoginChallenge { return op.challenge }
func NewRequire(token string, deps Dependencies) *requireLoginVerificationOperation {
	return &requireLoginVerificationOperation{SQL: deps.SQL, Dependencies: deps, token: token}
}
func (op *requireLoginVerificationOperation) Verification() *LoginVerification {
	return &LoginVerification{Flow: op.flow, State: op.state, payload: op.payload}
}
func NewCode(token, code, ip, ua string, deps Dependencies) *verifyLoginCodeOperation {
	return &verifyLoginCodeOperation{SQL: deps.SQL, Dependencies: deps, token: token, code: code, ip: ip, userAgent: ua}
}
func (op *verifyLoginCodeOperation) Bundle() *dto.AuthBundle { return op.bundle }
func NewComplete(token string, verification *LoginVerification, ip, ua string, deps Dependencies) *completeLoginVerificationOperation {
	op := &completeLoginVerificationOperation{SQL: deps.SQL, Dependencies: deps, verification: verification, ip: ip, userAgent: ua}
	op.loginCompletion.SQL = deps.SQL
	op.loginCompletion.Token = token
	op.loginCompletion.PayloadMatches = op.payloadMatches
	return op
}
func (op *completeLoginVerificationOperation) Bundle() *dto.AuthBundle { return op.bundle }
