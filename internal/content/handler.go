package content

import (
	"context"
	"encoding/json"
	"errors"
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

func errorsIs(err, target error) bool { return errors.Is(err, target) }
func errorsIsNotFound(err error) bool { return errors.Is(err, ErrNotFound) }

func notFoundOrInternal(err error) error {
	if errors.Is(err, ErrNotFound) {
		return apperr.New(apperr.CodeNotFound)
	}
	return err
}

// Handler mengekspos endpoint konten Learner dan CMS Admin.
type Handler struct {
	store *Store
}

func NewHandler(store *Store) *Handler {
	return &Handler{store: store}
}

// localeResolver menyimpan daftar Locale yang didukung untuk satu request.
type localeResolver struct {
	supported map[string]bool
	fallback  string
}

func (h *Handler) resolver(ctx context.Context) (localeResolver, error) {
	supported, fallback, err := h.store.SupportedLocales(ctx)
	if err != nil {
		return localeResolver{}, err
	}
	return localeResolver{supported: supported, fallback: fallback}, nil
}

func (r localeResolver) resolve(c *gin.Context) string {
	userLocale := ""
	if p, ok := auth.PrincipalFrom(c.Request.Context()); ok {
		// Preferensi user dimuat oleh layer auth bila tersedia; di sini cukup
		// memakai header dan query sesuai urutan Requirement 5.1.
		_ = p
	}
	return evaluation.ResolveLocale(c.Query("locale"), c.GetHeader("Accept-Language"),
		userLocale, r.fallback, r.supported)
}

func stringOf(m map[string]any, key string) string {
	v, ok := m[key]
	if !ok || v == nil {
		return ""
	}
	return fmt.Sprint(v)
}

func intOf(m map[string]any, key string) int {
	v, ok := m[key]
	if !ok || v == nil {
		return 0
	}
	switch x := v.(type) {
	case int:
		return x
	case int64:
		return int(x)
	default:
		return 0
	}
}

// --- Endpoint Learner (Requirement 10.2) ---

func (h *Handler) ListTopics(c *gin.Context) {
	h.listPublished(c, Resources["topics"], pagination.KindLearnerContent, nil)
}

func (h *Handler) GetTopic(c *gin.Context) {
	h.getPublished(c, Resources["topics"], c.Param("id"))
}

func (h *Handler) ListMaterials(c *gin.Context) {
	topicID, ok := parseID(c, c.Param("id"))
	if !ok {
		return
	}
	h.listPublished(c, Resources["materials"], pagination.KindLearnerContent, &topicID)
}

func (h *Handler) GetMaterial(c *gin.Context) {
	h.getPublished(c, Resources["materials"], c.Param("id"))
}

func (h *Handler) ListRoadmaps(c *gin.Context) {
	h.listPublished(c, Resources["roadmaps"], pagination.KindLearnerContent, nil)
}

func (h *Handler) GetRoadmap(c *gin.Context) {
	h.getPublished(c, Resources["roadmaps"], c.Param("id"))
}

func (h *Handler) listPublished(c *gin.Context, r Resource, kind pagination.ListKind, parentID *uuid.UUID) {
	resolver, err := h.resolver(c.Request.Context())
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	locale := resolver.resolve(c)
	req, err := pagination.ParseCursorQuery(kind, c.Query("limit"), c.Query("cursor"), 1)
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
	items, err := h.store.ListPublished(c.Request.Context(), r, locale, parentID, after, req.QueryLimit())
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	page, err := pagination.NewCursorPage(items, req.Limit, func(item map[string]any) []any {
		return []any{fmt.Sprint(item["id"])}
	})
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	c.Header("Content-Language", locale)
	c.JSON(http.StatusOK, page)
}

func (h *Handler) getPublished(c *gin.Context, r Resource, key string) {
	resolver, err := h.resolver(c.Request.Context())
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	locale := resolver.resolve(c)
	item, available, err := h.store.GetPublished(c.Request.Context(), r, key, locale)
	if err != nil {
		if errorsIsNotFound(err) {
			httpx.Fail(c, apperr.New(apperr.CodeNotFound))
		} else {
			httpx.Fail(c, err)
		}
		return
	}
	if item == nil {
		httpx.Fail(c, apperr.WithDetails(apperr.CodeTranslationNotAvailable,
			map[string]any{"available_locales": available}))
		return
	}
	c.Header("Content-Language", locale)
	c.JSON(http.StatusOK, item)
}

// --- Endpoint Admin CRUD (Requirement 10.6) ---

// RegisterRoutes memasang rute Learner dan Admin. Grup admin sudah memiliki
// middleware autentikasi dan RequireRole("admin") dari main.
func (h *Handler) RegisterRoutes(api *gin.RouterGroup, admin *gin.RouterGroup) {
	api.GET("/topics", h.ListTopics)
	api.GET("/topics/:id", h.GetTopic)
	api.GET("/topics/:id/materials", h.ListMaterials)
	api.GET("/materials/:id", h.GetMaterial)
	api.GET("/roadmaps", h.ListRoadmaps)
	api.GET("/roadmaps/:id", h.GetRoadmap)

	// Rute admin memakai prefiks statis per resource. Gin tidak mengizinkan
	// parameter (/admin/:resource) bercampur dengan rute statis (/admin/users)
	// pada level yang sama.
	for name := range Resources {
		n := name
		admin.GET("/"+n, withResource(n, h.listAdmin))
		admin.POST("/"+n, withResource(n, h.createAdmin))
		admin.GET("/"+n+"/:id", withResource(n, h.getAdmin))
		admin.PATCH("/"+n+"/:id", withResource(n, h.patchAdmin))
		admin.DELETE("/"+n+"/:id", withResource(n, h.deleteAdmin))
		admin.PUT("/"+n+"/:id/translations/:locale", withResource(n, h.putTranslation))
		admin.POST("/"+n+"/:id/translations/:locale/transitions", withResource(n, h.transitionTranslation))
	}
	admin.PUT("/topics/:id/prerequisites", h.setPrerequisites)
	admin.PUT("/questions/:id/concepts", h.setQuestionConcepts)
	admin.PUT("/roadmaps/:id/topics", h.setRoadmapTopics)
}

func withResource(name string, next gin.HandlerFunc) gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Params = append(c.Params, gin.Param{Key: "resource", Value: name})
		next(c)
	}
}

func resourceFrom(c *gin.Context) (Resource, bool) {
	r, ok := FindResource(c.Param("resource"))
	if !ok {
		httpx.Fail(c, apperr.New(apperr.CodeNotFound))
	}
	return r, ok
}

func parseID(c *gin.Context, raw string) (uuid.UUID, bool) {
	id, err := uuid.Parse(raw)
	if err != nil {
		httpx.Fail(c, apperr.Validation(apperr.FieldError{Field: "id", Reason: "uuid"}))
		return uuid.Nil, false
	}
	return id, true
}

func (h *Handler) listAdmin(c *gin.Context) {
	r, ok := resourceFrom(c)
	if !ok {
		return
	}
	req, err := pagination.ParseOffsetQuery(pagination.KindAdmin, c.Query("limit"), c.Query("offset"))
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	items, total, err := h.store.ListEntities(c.Request.Context(), r, req.Limit, req.Offset)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	c.JSON(http.StatusOK, pagination.NewOffsetPage(items, req, total))
}

func (h *Handler) createAdmin(c *gin.Context) {
	r, ok := resourceFrom(c)
	if !ok {
		return
	}
	var body map[string]any
	if err := httpx.BindJSON(c, &body); err != nil {
		httpx.Fail(c, err)
		return
	}
	fields := filterFields(body, AllowedEntityFields(r))
	item, err := h.store.CreateEntity(c.Request.Context(), r, fields)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	c.JSON(http.StatusCreated, item)
}

func (h *Handler) getAdmin(c *gin.Context) {
	r, ok := resourceFrom(c)
	if !ok {
		return
	}
	item, err := h.store.FindEntity(c.Request.Context(), r, c.Param("id"))
	if err != nil {
		httpx.Fail(c, notFoundOrInternal(err))
		return
	}
	c.JSON(http.StatusOK, item)
}

func (h *Handler) patchAdmin(c *gin.Context) {
	r, ok := resourceFrom(c)
	if !ok {
		return
	}
	id, ok := parseID(c, c.Param("id"))
	if !ok {
		return
	}
	var body map[string]any
	if err := httpx.BindJSON(c, &body); err != nil {
		httpx.Fail(c, err)
		return
	}
	// code tidak dapat diubah (Requirement 6.5).
	delete(body, "code")
	item, err := h.store.UpdateEntity(c.Request.Context(), r, id, filterFields(body, AllowedEntityFields(r)))
	if err != nil {
		httpx.Fail(c, notFoundOrInternal(err))
		return
	}
	c.JSON(http.StatusOK, item)
}

func (h *Handler) deleteAdmin(c *gin.Context) {
	r, ok := resourceFrom(c)
	if !ok {
		return
	}
	id, ok := parseID(c, c.Param("id"))
	if !ok {
		return
	}
	if err := h.store.DeleteEntity(c.Request.Context(), r, id); err != nil {
		httpx.Fail(c, notFoundOrInternal(err))
		return
	}
	c.Status(http.StatusNoContent)
}

func filterFields(body map[string]any, allowed map[string]bool) map[string]any {
	out := make(map[string]any)
	for k, v := range body {
		if allowed[k] {
			out[k] = v
		}
	}
	return out
}

type translationRequest struct {
	Version int            `json:"version"`
	Text    map[string]any `json:"text"`
}

func (h *Handler) putTranslation(c *gin.Context) {
	r, ok := resourceFrom(c)
	if !ok {
		return
	}
	id, ok := parseID(c, c.Param("id"))
	if !ok {
		return
	}
	locale := c.Param("locale")
	var req translationRequest
	if err := httpx.BindJSON(c, &req); err != nil {
		httpx.Fail(c, err)
		return
	}
	text := req.Text
	if text == nil {
		text = map[string]any{}
	}
	current, err := h.store.FindTranslation(c.Request.Context(), r, id, locale)
	if err != nil && !errorsIsNotFound(err) {
		httpx.Fail(c, err)
		return
	}
	merged := map[string]any{}
	for _, col := range r.TextColumns {
		merged[col] = text[col]
	}
	var state TranslationState
	if errorsIsNotFound(err) {
		state = NewTranslation("")
		if err := h.store.UpsertTranslation(c.Request.Context(), r, id, locale, merged, 0, state); err != nil {
			httpx.Fail(c, err)
			return
		}
	} else {
		version := intOf(current, "version")
		if req.Version != 0 && req.Version != version {
			httpx.Fail(c, apperr.New(apperr.CodeVersionConflict))
			return
		}
		state = TranslationState{
			Status:     stringOf(current, "status"),
			Version:    version,
			ReviewerID: stringOf(current, "reviewer_id"),
		}
		next, err := ApplyEdit(state, EditInput{ExpectedVersion: version, Text: ""})
		if err != nil {
			httpx.Fail(c, err)
			return
		}
		state = next
		if err := h.store.UpsertTranslation(c.Request.Context(), r, id, locale, merged, version, state); err != nil {
			if errorsIs(err, ErrVersionConflict) {
				httpx.Fail(c, apperr.New(apperr.CodeVersionConflict))
			} else {
				httpx.Fail(c, err)
			}
			return
		}
	}
	c.JSON(http.StatusOK, map[string]any{"version": state.Version, "status": state.Status, "locale": locale})
}

func (h *Handler) transitionTranslation(c *gin.Context) {
	r, ok := resourceFrom(c)
	if !ok {
		return
	}
	id, ok := parseID(c, c.Param("id"))
	if !ok {
		return
	}
	locale := c.Param("locale")
	var req struct {
		Target string `json:"target" binding:"required"`
	}
	if err := httpx.BindJSON(c, &req); err != nil {
		httpx.Fail(c, err)
		return
	}
	current, err := h.store.FindTranslation(c.Request.Context(), r, id, locale)
	if err != nil {
		httpx.Fail(c, notFoundOrInternal(err))
		return
	}
	p, _ := auth.PrincipalFrom(c.Request.Context())
	state := TranslationState{
		Status:     stringOf(current, "status"),
		Version:    intOf(current, "version"),
		ReviewerID: stringOf(current, "reviewer_id"),
	}
	now := time.Now().UTC()
	next, err := ApplyTransition(state, TransitionInput{
		Target: req.Target, ReviewerID: p.UserID, IsAdmin: true, Now: now,
	})
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	if r.Name == "questions" && req.Target == StatusPublished {
		can, err := h.store.QuestionCanPublish(c.Request.Context(), id)
		if err != nil {
			httpx.Fail(c, err)
			return
		}
		if !can {
			httpx.Fail(c, apperr.New(apperr.CodePublishRequirementsNotMet))
			return
		}
	}
	if err := h.store.SetTranslationStatus(c.Request.Context(), r, id, locale, next); err != nil {
		httpx.Fail(c, notFoundOrInternal(err))
		return
	}
	c.JSON(http.StatusOK, map[string]any{"version": next.Version, "status": next.Status, "locale": locale})
}

func (h *Handler) setPrerequisites(c *gin.Context) {
	id, ok := parseID(c, c.Param("id"))
	if !ok {
		return
	}
	var req struct {
		PrerequisiteIDs []string `json:"prerequisite_ids"`
	}
	if err := httpx.BindJSON(c, &req); err != nil {
		httpx.Fail(c, err)
		return
	}
	ids := make([]uuid.UUID, 0, len(req.PrerequisiteIDs))
	for _, raw := range req.PrerequisiteIDs {
		parsed, err := uuid.Parse(raw)
		if err != nil {
			httpx.Fail(c, apperr.Validation(apperr.FieldError{Field: "prerequisite_ids", Reason: "uuid"}))
			return
		}
		ids = append(ids, parsed)
	}
	err := h.store.SetPrerequisites(c.Request.Context(), id, ids, HasCycle)
	if errorsIs(err, ErrPrerequisiteCycle) {
		httpx.Fail(c, apperr.New(apperr.CodePrerequisiteCycle))
		return
	}
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	c.Status(http.StatusNoContent)
}

type weightedConcept struct {
	ConceptID  string `json:"concept_id" binding:"required"`
	Weight     string `json:"weight"`
	IsRequired bool   `json:"is_required"`
}

func (h *Handler) setQuestionConcepts(c *gin.Context) {
	questionID, ok := parseID(c, c.Param("id"))
	if !ok {
		return
	}
	var req struct {
		Concepts []weightedConcept `json:"concepts"`
	}
	if err := httpx.BindJSON(c, &req); err != nil {
		httpx.Fail(c, err)
		return
	}
	if err := h.store.replaceQuestionConcepts(c.Request.Context(), questionID, req.Concepts); err != nil {
		httpx.Fail(c, err)
		return
	}
	c.Status(http.StatusNoContent)
}

func (h *Handler) setRoadmapTopics(c *gin.Context) {
	roadmapID, ok := parseID(c, c.Param("id"))
	if !ok {
		return
	}
	var req struct {
		TopicIDs []string `json:"topic_ids"`
	}
	if err := httpx.BindJSON(c, &req); err != nil {
		httpx.Fail(c, err)
		return
	}
	ids := make([]uuid.UUID, 0, len(req.TopicIDs))
	for _, raw := range req.TopicIDs {
		parsed, err := uuid.Parse(raw)
		if err != nil {
			httpx.Fail(c, apperr.Validation(apperr.FieldError{Field: "topic_ids", Reason: "uuid"}))
			return
		}
		ids = append(ids, parsed)
	}
	if err := h.store.replaceRoadmapTopics(c.Request.Context(), roadmapID, ids); err != nil {
		httpx.Fail(c, err)
		return
	}
	c.Status(http.StatusNoContent)
}
