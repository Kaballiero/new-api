package release_unsubmitted_batch

import (
	"net/http"
	"strings"
	"unicode/utf8"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/internal/batch/domain"
	"github.com/QuantumNous/new-api/internal/batch/transport"

	"github.com/gin-gonic/gin"
)

type Audit func(*gin.Context, string, map[string]any, map[string]any, int, bool)
type Handler struct {
	usecase Usecase
	audit   Audit
}

func NewHandler(u Usecase, a Audit) Handler { return Handler{u, a} }
func (h Handler) Handle(c *gin.Context) {
	var request struct {
		Evidence string `json:"evidence"`
	}
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 32<<10)
	err := common.DecodeJson(c.Request.Body, &request)
	evidence := strings.TrimSpace(request.Evidence)
	if err != nil || evidence == "" || utf8.RuneCountInString(evidence) > 4096 {
		transport.Error(c, &domain.RuleError{Kind: domain.InvalidReleaseEvidence})
		return
	}
	outcome := "released"
	result, err := h.usecase.Execute(Input{c.Param("id"), c.GetInt("id")})
	defer func() {
		status := c.Writer.Status()
		if status == 404 {
			return
		}
		job := result.Job
		h.audit(c, "batch.release_unsubmitted", map[string]any{"id": c.Param("id"), "outcome": outcome}, map[string]any{"evidence": evidence, "released_quota": result.Release.ReleasedQuota, "user_id": job.UserID, "token_id": job.TokenID, "channel_id": job.ChannelID, "previous_last_error": result.Release.PreviousLastError}, status, status == 200 || status == 202)
	}()
	if err != nil {
		outcome = transport.Error(c, err)
		return
	}
	switch {
	case result.Release.Pending:
		outcome = "release_pending"
		c.JSON(202, gin.H{"success": true, "id": result.Job.PublicID, "status": outcome})
	case result.Release.AlreadyReleased:
		outcome = "already_released"
		c.JSON(200, gin.H{"success": true, "id": result.Job.PublicID, "already_released": true})
	default:
		c.JSON(200, gin.H{"success": true, "id": result.Job.PublicID, "released_quota": result.Release.ReleasedQuota})
	}
}
