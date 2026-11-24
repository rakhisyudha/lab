package progress

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/rakhisyudha/lab/internal/auth"
	"github.com/rakhisyudha/lab/internal/platform/httpx"
	"github.com/rakhisyudha/lab/internal/platform/httpx/apperr"
)

// Handler mengekspos endpoint progress Learner.
type Handler struct {
	store *Store
}

func NewHandler(store *Store) *Handler {
	return &Handler{store: store}
}

func userID(c *gin.Context) (uuid.UUID, bool) {
	p, ok := auth.PrincipalFrom(c.Request.Context())
	if !ok {
		httpx.Fail(c, apperr.New(apperr.CodeUnauthenticated))
		return uuid.Nil, false
	}
	id, err := uuid.Parse(p.UserID)
	if err != nil {
		httpx.Fail(c, apperr.New(apperr.CodeUnauthenticated))
		return uuid.Nil, false
	}
	return id, true
}

func (h *Handler) ListMastery(c *gin.Context) {
	uid, ok := userID(c)
	if !ok {
		return
	}
	items, err := h.store.ListMastery(c.Request.Context(), uid)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"items": items})
}

func (h *Handler) GetMastery(c *gin.Context) {
	uid, ok := userID(c)
	if !ok {
		return
	}
	topicID, err := uuid.Parse(c.Param("topic_id"))
	if err != nil {
		httpx.Fail(c, apperr.Validation(apperr.FieldError{Field: "topic_id", Reason: "uuid"}))
		return
	}
	exists, err := h.store.TopicExists(c.Request.Context(), topicID)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	if !exists {
		httpx.Fail(c, apperr.New(apperr.CodeNotFound))
		return
	}
	view, err := h.store.GetMastery(c.Request.Context(), uid, topicID)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	c.JSON(http.StatusOK, view)
}

func (h *Handler) ListMisconceptions(c *gin.Context) {
	uid, ok := userID(c)
	if !ok {
		return
	}
	status := c.Query("status")
	if status != "" && status != "detected" && status != "resolved" {
		httpx.Fail(c, apperr.Validation(apperr.FieldError{Field: "status", Reason: "oneof_detected_resolved"}))
		return
	}
	items, err := h.store.ListMisconceptions(c.Request.Context(), uid, status)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"items": items})
}

// RegisterRoutes memasang endpoint pada grup yang sudah terautentikasi.
func (h *Handler) RegisterRoutes(me *gin.RouterGroup) {
	me.GET("/progress", h.ListMastery)
	me.GET("/progress/topics/:topic_id", h.GetMastery)
	me.GET("/misconceptions", h.ListMisconceptions)
}
