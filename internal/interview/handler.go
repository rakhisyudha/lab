package interview

import (
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/rakhisyudha/lab/internal/auth"
	"github.com/rakhisyudha/lab/internal/evaluation"
	"github.com/rakhisyudha/lab/internal/platform/httpx"
	"github.com/rakhisyudha/lab/internal/platform/httpx/apperr"
	"github.com/rakhisyudha/lab/internal/platform/pagination"
)

type Handler struct {
	service   *Service
	store     *Store
	finalizer *Finalizer
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

func (h *Handler) Profiles(c *gin.Context) {
	items, err := h.store.ListProfiles(c.Request.Context())
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"items": items})
}

func (h *Handler) Create(c *gin.Context) {
	uid, ok := principalID(c)
	if !ok {
		return
	}
	var req struct {
		InterviewType string   `json:"interview_type" binding:"required"`
		TopicIDs      []string `json:"topic_ids" binding:"required"`
		Locale        string   `json:"locale"`
		RetryOf       string   `json:"retry_of_session_id"`
	}
	if err := httpx.BindJSON(c, &req); err != nil {
		httpx.Fail(c, err)
		return
	}
	topicIDs := make([]uuid.UUID, 0, len(req.TopicIDs))
	for _, raw := range req.TopicIDs {
		id, err := uuid.Parse(raw)
		if err != nil {
			httpx.Fail(c, apperr.Validation(apperr.FieldError{Field: "topic_ids", Reason: "uuid"}))
			return
		}
		topicIDs = append(topicIDs, id)
	}
	var retryOf *uuid.UUID
	if req.RetryOf != "" {
		if id, err := uuid.Parse(req.RetryOf); err == nil {
			retryOf = &id
		}
	}
	locale := req.Locale
	if locale == "" {
		locale = "id"
	}
	id, err := h.service.Create(c.Request.Context(), uid, req.InterviewType, topicIDs, locale, retryOf)
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
		if id, err := uuid.Parse(raw); err == nil {
			after = &id
		}
	}
	items, err := h.store.ListSessions(c.Request.Context(), uid, after, req.QueryLimit())
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	page, err := pagination.NewCursorPage(items, req.Limit, func(s Session) []any { return []any{s.ID.String()} })
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
	sess, err := h.store.GetSession(c.Request.Context(), id)
	if err != nil || sess.UserID != uid {
		httpx.Fail(c, apperr.New(apperr.CodeNotFound))
		return
	}
	c.JSON(http.StatusOK, sess)
}

func (h *Handler) Messages(c *gin.Context) {
	uid, ok := principalID(c)
	if !ok {
		return
	}
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		httpx.Fail(c, apperr.Validation(apperr.FieldError{Field: "id", Reason: "uuid"}))
		return
	}
	sess, err := h.store.GetSession(c.Request.Context(), id)
	if err != nil || sess.UserID != uid {
		httpx.Fail(c, apperr.New(apperr.CodeNotFound))
		return
	}
	after := 0
	if raw := c.Query("cursor"); raw != "" {
		var s string
		if err := json.Unmarshal([]byte(raw), &s); err == nil {
			_, _ = fmt.Sscanf(s, "%d", &after)
		}
	}
	limit := 50
	items, err := h.store.ListMessages(c.Request.Context(), id, after, limit+1)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	page, err := pagination.NewCursorPage(items, limit, func(m Message) []any { return []any{m.Sequence} })
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	c.JSON(http.StatusOK, page)
}

func (h *Handler) lifecycle(action string) gin.HandlerFunc {
	return func(c *gin.Context) {
		uid, ok := principalID(c)
		if !ok {
			return
		}
		id, err := uuid.Parse(c.Param("id"))
		if err != nil {
			httpx.Fail(c, apperr.Validation(apperr.FieldError{Field: "id", Reason: "uuid"}))
			return
		}
		switch action {
		case "start":
			err = h.service.Start(c.Request.Context(), id, uid)
		case "pause":
			err = h.service.Pause(c.Request.Context(), id, uid)
		case "resume":
			err = h.service.Resume(c.Request.Context(), id, uid)
		case "abandon":
			err = h.service.Abandon(c.Request.Context(), id, uid)
		}
		if err != nil {
			httpx.Fail(c, err)
			return
		}
		c.Status(http.StatusNoContent)
	}
}

func (h *Handler) Answer(c *gin.Context) {
	uid, ok := principalID(c)
	if !ok {
		return
	}
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		httpx.Fail(c, apperr.Validation(apperr.FieldError{Field: "id", Reason: "uuid"}))
		return
	}
	var req struct {
		QuestionID string `json:"question_id" binding:"required"`
		ParentID   string `json:"parent_message_id"`
		Content    string `json:"content" binding:"required"`
	}
	if err := httpx.BindJSON(c, &req); err != nil {
		httpx.Fail(c, err)
		return
	}
	qid, err := uuid.Parse(req.QuestionID)
	if err != nil {
		httpx.Fail(c, apperr.Validation(apperr.FieldError{Field: "question_id", Reason: "uuid"}))
		return
	}
	taskID, err := h.service.SubmitAnswer(c.Request.Context(), id, uid, qid, req.ParentID, req.Content)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	c.JSON(http.StatusAccepted, gin.H{"ai_task_id": taskID.String()})
}

// RegisterRoutes memasang endpoint interview. finalizerOps boleh nil.
func (h *Handler) RegisterRoutes(api *gin.RouterGroup, mw gin.HandlerFunc, evalSvc *EvaluationService, fin *Finalizer) {
	h.finalizer = fin
	g := api.Group("/interviews", mw)
	{
		g.GET("", h.List)
		g.POST("", h.Create)
		g.GET("/:id", h.Get)
		g.GET("/:id/messages", h.Messages)
		g.POST("/:id/start", h.lifecycle("start"))
		g.POST("/:id/pause", h.lifecycle("pause"))
		g.POST("/:id/resume", h.lifecycle("resume"))
		g.POST("/:id/abandon", h.lifecycle("abandon"))
		g.POST("/:id/finish", func(c *gin.Context) {
			uid, ok := principalID(c)
			if !ok {
				return
			}
			id, err := uuid.Parse(c.Param("id"))
			if err != nil {
				httpx.Fail(c, apperr.Validation(apperr.FieldError{Field: "id", Reason: "uuid"}))
				return
			}
			if err := h.service.transition(c.Request.Context(), id, uid, evaluation.SessionAction("finish")); err != nil {
				httpx.Fail(c, err)
				return
			}
			sess, err := h.store.GetSession(c.Request.Context(), id)
			if err == nil && h.finalizer != nil {
				_ = h.finalizer.Finalize(c.Request.Context(), sess, time.Now().UTC())
			}
			c.JSON(http.StatusAccepted, gin.H{"id": id.String(), "status": "COMPLETED"})
		})
		g.POST("/:id/answers", h.Answer)
		g.GET("/:id/evaluation", func(c *gin.Context) {
			uid, ok := principalID(c)
			if !ok {
				return
			}
			id, err := uuid.Parse(c.Param("id"))
			if err != nil {
				httpx.Fail(c, apperr.Validation(apperr.FieldError{Field: "id", Reason: "uuid"}))
				return
			}
			sess, err := h.store.GetSession(c.Request.Context(), id)
			if err != nil || sess.UserID != uid {
				httpx.Fail(c, apperr.New(apperr.CodeNotFound))
				return
			}
			report, err := evalSvc.Report(c.Request.Context(), sess)
			if err != nil {
				httpx.Fail(c, err)
				return
			}
			c.JSON(http.StatusOK, report)
		})
		g.POST("/:id/evaluation/retry", func(c *gin.Context) {
			uid, ok := principalID(c)
			if !ok {
				return
			}
			id, err := uuid.Parse(c.Param("id"))
			if err != nil {
				httpx.Fail(c, apperr.Validation(apperr.FieldError{Field: "id", Reason: "uuid"}))
				return
			}
			taskID, err := evalSvc.RetrySessionEvaluation(c.Request.Context(), id, uid)
			if err != nil {
				httpx.Fail(c, err)
				return
			}
			c.JSON(http.StatusAccepted, gin.H{"ai_task_id": taskID.String()})
		})
		g.POST("/:id/messages/:message_id/evaluation/retry", func(c *gin.Context) {
			uid, ok := principalID(c)
			if !ok {
				return
			}
			id, err := uuid.Parse(c.Param("id"))
			if err != nil {
				httpx.Fail(c, apperr.Validation(apperr.FieldError{Field: "id", Reason: "uuid"}))
				return
			}
			msgID, err := uuid.Parse(c.Param("message_id"))
			if err != nil {
				httpx.Fail(c, apperr.Validation(apperr.FieldError{Field: "message_id", Reason: "uuid"}))
				return
			}
			status, err := evalSvc.RetryAnswerEvaluation(c.Request.Context(), id, msgID, uid)
			if err != nil {
				httpx.Fail(c, err)
				return
			}
			c.JSON(status, gin.H{"status": "pending"})
		})
	}
	api.GET("/interview-profiles", mw, h.Profiles)
}
