package assessment

import (
	"encoding/json"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/rakhisyudha/lab/internal/auth"
	"github.com/rakhisyudha/lab/internal/platform/httpx"
	"github.com/rakhisyudha/lab/internal/platform/httpx/apperr"
	"github.com/rakhisyudha/lab/internal/platform/pagination"
)

type Handler struct {
	service *Service
	store   *Store
}

func NewHandler(service *Service, store *Store) *Handler {
	return &Handler{service: service, store: store}
}

func principalID(c *gin.Context) (uuid.UUID, bool) {
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

func (h *Handler) Create(c *gin.Context) {
	uid, ok := principalID(c)
	if !ok {
		return
	}
	var req struct {
		TopicID string `json:"topic_id" binding:"required"`
		Locale  string `json:"locale"`
	}
	if err := httpx.BindJSON(c, &req); err != nil {
		httpx.Fail(c, err)
		return
	}
	topicID, err := uuid.Parse(req.TopicID)
	if err != nil {
		httpx.Fail(c, apperr.Validation(apperr.FieldError{Field: "topic_id", Reason: "uuid"}))
		return
	}
	locale := req.Locale
	if locale == "" {
		locale = "id"
	}
	id, err := h.service.Create(c.Request.Context(), uid, topicID, locale)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	c.JSON(http.StatusCreated, gin.H{"id": id.String()})
}

func (h *Handler) List(c *gin.Context) {
	uid, ok := principalID(c)
	if !ok {
		return
	}
	req, err := pagination.ParseCursorQuery(pagination.KindSessionList, c.Query("limit"), c.Query("cursor"), 1)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	var after *uuid.UUID
	if len(req.Keys) > 0 {
		var raw string
		if err := json.Unmarshal(req.Keys[0], &raw); err != nil {
			httpx.Fail(c, apperr.Validation(apperr.FieldError{Field: "cursor", Reason: "invalid_cursor"}))
			return
		}
		id, err := uuid.Parse(raw)
		if err != nil {
			httpx.Fail(c, apperr.Validation(apperr.FieldError{Field: "cursor", Reason: "invalid_cursor"}))
			return
		}
		after = &id
	}
	items, err := h.store.ListAttempts(c.Request.Context(), uid, after, req.QueryLimit())
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	page, err := pagination.NewCursorPage(items, req.Limit, func(a AttemptRow) []any {
		return []any{a.ID.String()}
	})
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	c.JSON(http.StatusOK, page)
}

func (h *Handler) Get(c *gin.Context) {
	uid, ok := principalID(c)
	if !ok {
		return
	}
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		httpx.Fail(c, apperr.Validation(apperr.FieldError{Field: "id", Reason: "uuid"}))
		return
	}
	attempt, err := h.store.GetAttempt(c.Request.Context(), id)
	if err != nil || attempt.UserID != uid {
		httpx.Fail(c, apperr.New(apperr.CodeNotFound))
		return
	}
	answers, err := h.store.ListAnswers(c.Request.Context(), id)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"attempt": attempt,
		"answers": answers,
	})
}

func (h *Handler) SaveAnswer(c *gin.Context) {
	uid, ok := principalID(c)
	if !ok {
		return
	}
	attemptID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		httpx.Fail(c, apperr.Validation(apperr.FieldError{Field: "id", Reason: "uuid"}))
		return
	}
	questionID, err := uuid.Parse(c.Param("question_id"))
	if err != nil {
		httpx.Fail(c, apperr.Validation(apperr.FieldError{Field: "question_id", Reason: "uuid"}))
		return
	}
	attempt, err := h.store.GetAttempt(c.Request.Context(), attemptID)
	if err != nil || attempt.UserID != uid {
		httpx.Fail(c, apperr.New(apperr.CodeNotFound))
		return
	}
	var req struct {
		AnswerText string `json:"answer_text"`
	}
	if err := httpx.BindJSON(c, &req); err != nil {
		httpx.Fail(c, err)
		return
	}
	if err := h.service.SaveAnswer(c.Request.Context(), attemptID, questionID, req.AnswerText); err != nil {
		httpx.Fail(c, err)
		return
	}
	c.Status(http.StatusNoContent)
}

func (h *Handler) Submit(c *gin.Context) {
	uid, ok := principalID(c)
	if !ok {
		return
	}
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		httpx.Fail(c, apperr.Validation(apperr.FieldError{Field: "id", Reason: "uuid"}))
		return
	}
	attempt, err := h.store.GetAttempt(c.Request.Context(), id)
	if err != nil || attempt.UserID != uid {
		httpx.Fail(c, apperr.New(apperr.CodeNotFound))
		return
	}
	if err := h.service.Submit(c.Request.Context(), id, uid, c.GetHeader("Idempotency-Key")); err != nil {
		httpx.Fail(c, err)
		return
	}
	c.JSON(http.StatusAccepted, gin.H{"id": id.String(), "status": "submitted"})
}

func (h *Handler) Retry(c *gin.Context) {
	uid, ok := principalID(c)
	if !ok {
		return
	}
	attemptID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		httpx.Fail(c, apperr.Validation(apperr.FieldError{Field: "id", Reason: "uuid"}))
		return
	}
	questionID, err := uuid.Parse(c.Param("question_id"))
	if err != nil {
		httpx.Fail(c, apperr.Validation(apperr.FieldError{Field: "question_id", Reason: "uuid"}))
		return
	}
	status, err := h.service.RetryEvaluation(c.Request.Context(), attemptID, questionID, uid)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	c.JSON(status, gin.H{"status": "pending"})
}

func (h *Handler) RegisterRoutes(api *gin.RouterGroup, mw gin.HandlerFunc) {
	g := api.Group("/assessments", mw)
	{
		g.POST("/attempts", h.Create)
		g.GET("/attempts", h.List)
		g.GET("/attempts/:id", h.Get)
		g.PUT("/attempts/:id/answers/:question_id", h.SaveAnswer)
		g.POST("/attempts/:id/submit", h.Submit)
		g.POST("/attempts/:id/answers/:question_id/evaluation/retry", h.Retry)
	}
}
