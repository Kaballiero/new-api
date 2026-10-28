package list_custom_oauth_providers

import (
	"net/http"

	"github.com/QuantumNous/new-api/internal/identity/domain"
	"github.com/QuantumNous/new-api/internal/identity/dto"
	"github.com/QuantumNous/new-api/internal/identity/transport"

	"github.com/gin-gonic/gin"
)

type Reader interface {
	List() ([]domain.CustomOAuthProvider, error)
}

type HTTP struct{ reader Reader }

func NewHTTP(reader Reader) *HTTP { return &HTTP{reader: reader} }
func (h *HTTP) Handle(c *gin.Context) {
	providers, err := h.reader.List()
	if err != nil {
		transport.WriteCustomOAuthError(c, err)
		return
	}
	response := make([]*dto.CustomOAuthProviderResponse, len(providers))
	for i, provider := range providers {
		response[i] = dto.CustomOAuthProviderResponseFrom(provider)
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "message": "", "data": response})
}
