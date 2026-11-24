package progress

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/rakhisyudha/lab/internal/auth"
	"github.com/rakhisyudha/lab/internal/platform/httpx"
	"github.com/rakhisyudha/lab/internal/platform/httpx/apperr"
)

// RegisterAdminRoutes memasang resolusi manual Misconception bagi Admin.
func (h *Handler) RegisterAdminRoutes(admin *gin.RouterGroup, mastery *MasteryService) {
	admin.POST("/user-misconceptions/:id/resolve", func(c *gin.Context) {
		id, err := uuid.Parse(c.Param("id"))
		if err != nil {
			httpx.Fail(c, apperr.Validation(apperr.FieldError{Field: "id", Reason: "uuid"}))
			return
		}
		var req struct {
			Reason string `json:"reason" binding:"required"`
		}
		if err := httpx.BindJSON(c, &req); err != nil {
			httpx.Fail(c, err)
			return
		}
		p, _ := auth.PrincipalFrom(c.Request.Context())
		actor, err := uuid.Parse(p.UserID)
		if err != nil {
			httpx.Fail(c, apperr.New(apperr.CodeUnauthenticated))
			return
		}
		userID, topicID, err := mastery.ResolveManual(c.Request.Context(), id, actor, req.Reason)
		if err != nil {
			httpx.Fail(c, err)
			return
		}
		c.JSON(http.StatusOK, gin.H{"user_id": userID.String(), "topic_id": topicID.String()})
	})
}
