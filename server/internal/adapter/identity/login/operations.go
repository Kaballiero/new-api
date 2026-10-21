package login

import (
	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/internal/identity/dto"
	"github.com/QuantumNous/new-api/model"
	"time"
)

type Dependencies struct {
	TTL                     time.Duration
	PolicyFunc              func(string, model.UserVerificationState) ([]dto.VerificationMethodOption, error)
	RequireMethodFunc       func(*model.UserVerificationState, string) error
	RequireVerificationFunc func(string, string) (*LoginVerification, error)
	VerifyFactorFunc        func(*model.TwoFA, string) error
	CompleteFunc            func(string, *LoginVerification, string, string, string) (*dto.AuthBundle, error)
	NewSessionFunc          func(int, int64, string, string, string) (*model.UserSession, string, error)
	IssueFunc               func(*model.UserSession, string, bool) (*dto.AuthBundle, error)
}
type loginFlowPayload struct {
	AuthVersion int64  `json:"auth_version"`
	LoginMethod string `json:"login_method"`
}

// LoginVerification is server-owned state read from a primary-authenticated flow.
// It is never constructed from a user ID or an authentication claim in a request.
type LoginVerification struct {
	Flow    *model.AuthFlow
	State   *model.UserVerificationState
	payload loginFlowPayload
}

type startLoginVerificationOperation struct {
	Dependencies
	primary     *model.User
	loginMethod string
	state       *model.UserVerificationState
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
	op.state, err = model.GetUserVerificationState(op.primary.Id)
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
	token, _, err := model.CreateAuthFlow(model.AuthFlowCreate{Purpose: model.AuthFlowPurposeLoginVerification, UserId: op.primary.Id, Payload: string(payload), ExpiresAt: expiresAt})
	if err != nil {
		return err
	}
	op.challenge = &dto.LoginChallenge{RequireVerification: true, FlowToken: token, ExpiresAt: expiresAt.Unix(), Methods: op.methods}
	return nil
}

type requireLoginVerificationOperation struct {
	Dependencies
	token   string
	flow    *model.AuthFlow
	state   *model.UserVerificationState
	payload loginFlowPayload
}

func (op *requireLoginVerificationOperation) LoadFlow() error {
	var err error
	op.flow, err = model.GetAuthFlow(op.token, model.AuthFlowMatch{Purpose: model.AuthFlowPurposeLoginVerification})
	return err
}

func (op *requireLoginVerificationOperation) DecodePayload() (int64, string, error) {
	err := common.UnmarshalJsonStr(op.flow.Payload, &op.payload)
	return op.payload.AuthVersion, op.payload.LoginMethod, err
}

func (op *requireLoginVerificationOperation) LoadState() (int, int64, error) {
	var err error
	op.state, err = model.GetUserVerificationState(op.flow.UserId)
	if err != nil {
		return 0, 0, err
	}
	return op.state.Status, op.state.AuthVersion, nil
}

func (op *requireLoginVerificationOperation) RequireMethod(method string) error {
	return op.RequireMethodFunc(op.state, method)
}

type verifyLoginCodeOperation struct {
	Dependencies
	token, code, ip, userAgent string
	verification               *LoginVerification
	factor                     *model.TwoFA
	bundle                     *dto.AuthBundle
}

func (op *verifyLoginCodeOperation) RequireVerification() error {
	var err error
	op.verification, err = op.RequireVerificationFunc(op.token, "2fa")
	return err
}

func (op *verifyLoginCodeOperation) LoadFactor() error {
	var err error
	op.factor, err = model.GetTwoFAByUserId(op.verification.State.UserID)
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

func (op *completeLoginVerificationOperation) payloadMatches(flow *model.AuthFlow) bool {
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
	_, err := model.RevokeUserSession(op.Session.UserID, op.Session.SID, reason)
	return err
}

type loginCompletion struct {
	Token          string
	Session        *model.UserSession
	CurrentState   *model.UserVerificationState
	PayloadMatches func(*model.AuthFlow) bool
}

func (op *loginCompletion) ConsumeAndCreate(check func(int64, bool) error) error {
	if op.PayloadMatches == nil || check == nil {
		return model.ErrUserSessionInvalid
	}
	return model.CreateUserSessionFromLoginFlow(op.Token, op.Session, func(flow *model.AuthFlow, state *model.UserVerificationState) error {
		op.CurrentState = state
		return check(flow.Id, op.PayloadMatches(flow))
	})
}

func NewStart(user *model.User, method string, deps Dependencies) *startLoginVerificationOperation {
	return &startLoginVerificationOperation{Dependencies: deps, primary: user, loginMethod: method}
}
func (op *startLoginVerificationOperation) Challenge() *dto.LoginChallenge { return op.challenge }
func NewRequire(token string, deps Dependencies) *requireLoginVerificationOperation {
	return &requireLoginVerificationOperation{Dependencies: deps, token: token}
}
func (op *requireLoginVerificationOperation) Verification() *LoginVerification {
	return &LoginVerification{Flow: op.flow, State: op.state, payload: op.payload}
}
func NewCode(token, code, ip, ua string, deps Dependencies) *verifyLoginCodeOperation {
	return &verifyLoginCodeOperation{Dependencies: deps, token: token, code: code, ip: ip, userAgent: ua}
}
func (op *verifyLoginCodeOperation) Bundle() *dto.AuthBundle { return op.bundle }
func NewComplete(token string, verification *LoginVerification, ip, ua string, deps Dependencies) *completeLoginVerificationOperation {
	op := &completeLoginVerificationOperation{Dependencies: deps, verification: verification, ip: ip, userAgent: ua}
	op.loginCompletion.Token = token
	op.loginCompletion.PayloadMatches = op.payloadMatches
	return op
}
func (op *completeLoginVerificationOperation) Bundle() *dto.AuthBundle { return op.bundle }
