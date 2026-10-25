package prepare_batch

import (
	"encoding/json"
	"github.com/QuantumNous/new-api/i18n"
	"net/http"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/internal/batch/domain"
	"github.com/QuantumNous/new-api/internal/batch/dto"
	"github.com/QuantumNous/new-api/internal/batch/transport"
	"github.com/gin-gonic/gin"
)

type Handler struct {
	usecase   Usecase
	constrain func(*gin.Context, string)
}

func NewHandler(u Usecase, constrain func(*gin.Context, string)) Handler {
	return Handler{u, constrain}
}
func (h Handler) Handle(c *gin.Context) {
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, domain.MaxBatchRequestBytes)
	var request domain.CreateRequest
	var envelope json.RawMessage
	if err := common.UnmarshalBodyReusable(c, &envelope); err != nil || common.Unmarshal(envelope, &request) != nil {
		transport.Error(c, &domain.RuleError{Kind: domain.MalformedEnvelope})
		return
	}
	prepared, err := h.usecase.Execute(c, Input{Request: request, Envelope: envelope, UserID: c.GetInt("id"),
		IdempotencyKey: c.GetHeader("Idempotency-Key")})
	if err != nil {
		transport.Error(c, err)
		return
	}
	if prepared.Existing != nil {
		c.AbortWithStatusJSON(200, dto.Job(*prepared.Existing).WithLanguage(i18n.GetLangFromContext(c)))
		return
	}
	c.Set("batch_public_id", prepared.PublicID)
	c.Set("batch_key_hash", prepared.KeyHash)
	c.Set("batch_request_hash", prepared.RequestHash)
	c.Set("batch_create_request", prepared.Request)
	h.constrain(c, prepared.Request.Endpoint)
}
