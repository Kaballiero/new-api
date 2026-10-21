package passkeyflow

import (
	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/internal/identity/domain"
	"github.com/QuantumNous/new-api/model"
	webauthn "github.com/go-webauthn/webauthn/webauthn"
	"gorm.io/gorm"
	"time"
)

type Payload struct {
	SessionData webauthn.SessionData       `json:"session_data"`
	Security    domain.PasskeyFlowSecurity `json:"security"`
}
type Creation struct {
	Purpose  string
	Security domain.PasskeyFlowSecurity
	Data     *webauthn.SessionData
	Token    string
	payload  []byte
}

func (op *Creation) EncodePayload() error {
	var err error
	op.payload, err = common.Marshal(Payload{SessionData: *op.Data, Security: op.Security})
	return err
}
func (op *Creation) Create(expires time.Time) error {
	token, _, err := model.CreateAuthFlow(model.AuthFlowCreate{Purpose: op.Purpose, UserId: op.Security.UserID, SessionId: op.Security.SessionID, Payload: string(op.payload), ExpiresAt: expires})
	op.Token = token
	return err
}

type Consumption struct {
	Token, Purpose string
	Identity       domain.AuthIdentity
	Payload        Payload
	tx             *gorm.DB
}

func (op *Consumption) Consume(check func(domain.PasskeyFlowSecurity) error) error {
	_, err := model.ConsumeAuthFlowWithAction(op.Token, model.AuthFlowMatch{Purpose: op.Purpose, UserId: op.Identity.UserID, SessionId: op.Identity.SessionID}, func(tx *gorm.DB, flow *model.AuthFlow) error {
		if err := common.UnmarshalJsonStr(flow.Payload, &op.Payload); err != nil {
			return err
		}
		op.tx = tx
		defer func() { op.tx = nil }()
		return check(op.Payload.Security)
	})
	return err
}
func (op *Consumption) ValidateCurrentSession() error {
	return model.ValidateAuthSessionWithTx(op.tx, op.Identity)
}
