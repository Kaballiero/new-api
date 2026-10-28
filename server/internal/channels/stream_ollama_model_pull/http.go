package stream_ollama_model_pull

import (
	"errors"
	"fmt"
	"net/http"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/internal/channels/domain"
	"github.com/QuantumNous/new-api/internal/channels/transport"

	"github.com/gin-gonic/gin"
)

type HTTP struct {
	store    func() Store
	provider Provider
}

func NewHTTP(store func() Store, provider Provider) HTTP {
	return HTTP{store: store, provider: provider}
}
func (h HTTP) Handle(c *gin.Context) {
	var req Input
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "Invalid request parameters"})
		return
	}
	scenario := NewUsecase(h.store(), h.provider, func() {
		c.Header("Content-Type", "text/event-stream")
		c.Header("Cache-Control", "no-cache")
		c.Header("Connection", "keep-alive")
		c.Header("Access-Control-Allow-Origin", "*")
	}, func(progress domain.PullProgress) {
		data, _ := common.Marshal(progress)
		fmt.Fprintf(c.Writer, "data: %s\n\n", string(data))
		c.Writer.Flush()
	})
	err := scenario.Execute(req)
	if err != nil && !errors.Is(err, domain.ErrOllamaStreamFailed) {
		transport.WriteError(c, err)
		return
	}
	if err != nil {
		data, _ := common.Marshal(gin.H{"error": err.Error()})
		fmt.Fprintf(c.Writer, "data: %s\n\n", string(data))
	} else {
		data, _ := common.Marshal(gin.H{"message": fmt.Sprintf("Model %s pulled successfully", req.ModelName)})
		fmt.Fprintf(c.Writer, "data: %s\n\n", string(data))
	}
	fmt.Fprintf(c.Writer, "data: [DONE]\n\n")
	c.Writer.Flush()
}
