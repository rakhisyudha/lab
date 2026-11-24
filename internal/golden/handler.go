package golden

import (
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/rakhisyudha/lab/internal/auth"
	"github.com/rakhisyudha/lab/internal/platform/httpx"
	"github.com/rakhisyudha/lab/internal/platform/httpx/apperr"
	"github.com/rakhisyudha/lab/internal/platform/pagination"
)

// Handler mengekspos endpoint Admin Golden_Set.
type Handler struct {
	store  *Store
	runner *Runner
}

func NewHandler(store *Store, runner *Runner) *Handler {
	return &Handler{store: store, runner: runner}
}

type caseRequest struct {
	QuestionID             string   `json:"question_id" binding:"required"`
	Locale                 string   `json:"locale" binding:"required"`
	Category               string   `json:"category" binding:"required"`
	SampleAnswer           string   `json:"sample_answer"`
	ExpectedScoreMin       float64  `json:"expected_score_min"`
	ExpectedScoreMax       float64  `json:"expected_score_max"`
	ExpectedFound          []string `json:"expected_found_concept_codes"`
	ExpectedMissed         []string `json:"expected_missed_concept_codes"`
	ExpectedMisconceptions []string `json:"expected_misconception_codes"`
}

func (h *Handler) CreateCase(c *gin.Context) {
	var req caseRequest
	if err := httpx.BindJSON(c, &req); err != nil {
		httpx.Fail(c, err)
		return
	}
	qid, err := uuid.Parse(req.QuestionID)
	if err != nil {
		httpx.Fail(c, apperr.Validation(apperr.FieldError{Field: "question_id", Reason: "uuid"}))
		return
	}
	catalog, concepts, locales, _, err := h.store.CaseLabels(c.Request.Context(), qid)
	if err != nil {
		httpx.Fail(c, apperr.New(apperr.CodeNotFound))
		return
	}
	in := CaseInput(req)
	if err := ValidateCase(in, catalog, concepts, locales); err != nil {
		httpx.Fail(c, err)
		return
	}
	p, _ := auth.PrincipalFrom(c.Request.Context())
	owner, _ := uuid.Parse(p.UserID)
	id, err := h.store.CreateCase(c.Request.Context(), in, owner)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	c.JSON(http.StatusCreated, gin.H{"id": id.String()})
}

func (h *Handler) ListCases(c *gin.Context) {
	req, err := pagination.ParseOffsetQuery(pagination.KindAdmin, c.Query("limit"), c.Query("offset"))
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	items, err := h.store.ListCases(c.Request.Context())
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	total := int64(len(items))
	if req.Offset < len(items) {
		end := req.Offset + req.Limit
		if end > len(items) {
			end = len(items)
		}
		items = items[req.Offset:end]
	}
	c.JSON(http.StatusOK, pagination.NewOffsetPage(items, req, total))
}

func (h *Handler) PatchCase(c *gin.Context) {
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		httpx.Fail(c, apperr.Validation(apperr.FieldError{Field: "id", Reason: "uuid"}))
		return
	}
	var body map[string]any
	if err := httpx.BindJSON(c, &body); err != nil {
		httpx.Fail(c, err)
		return
	}
	fields := map[string]any{}
	for _, k := range []string{"sample_answer", "expected_score_min", "expected_score_max", "is_active"} {
		if v, ok := body[k]; ok {
			fields[k] = v
		}
	}
	if err := h.store.UpdateCase(c.Request.Context(), id, fields); err != nil {
		httpx.Fail(c, notFoundOrInternal(err))
		return
	}
	c.Status(http.StatusNoContent)
}

func (h *Handler) DeleteCase(c *gin.Context) {
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		httpx.Fail(c, apperr.Validation(apperr.FieldError{Field: "id", Reason: "uuid"}))
		return
	}
	if err := h.store.DeleteCase(c.Request.Context(), id); err != nil {
		httpx.Fail(c, notFoundOrInternal(err))
		return
	}
	c.Status(http.StatusNoContent)
}

type runRequest struct {
	PromptContractID string `json:"prompt_contract_id" binding:"required"`
	RunMode          string `json:"run_mode" binding:"required,oneof=recorded live"`
	ModelName        string `json:"model_name"`
	TriggerSource    string `json:"trigger_source"`
}

func (h *Handler) CreateRun(c *gin.Context) {
	var req runRequest
	if err := httpx.BindJSON(c, &req); err != nil {
		httpx.Fail(c, err)
		return
	}
	contractID, err := uuid.Parse(req.PromptContractID)
	if err != nil {
		httpx.Fail(c, apperr.Validation(apperr.FieldError{Field: "prompt_contract_id", Reason: "uuid"}))
		return
	}
	model := req.ModelName
	if model == "" {
		model = "replay"
	}
	trigger := req.TriggerSource
	if trigger == "" {
		trigger = "manual"
	}
	id, err := h.runner.Run(c.Request.Context(), contractID, model, req.RunMode, trigger)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	c.JSON(http.StatusAccepted, gin.H{"id": id.String(), "status": "running"})
}

func (h *Handler) ListRuns(c *gin.Context) {
	items, err := h.store.ListRuns(c.Request.Context())
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"items": items})
}

func notFoundOrInternal(err error) error {
	if errorsIs(err, ErrNotFound) {
		return apperr.New(apperr.CodeNotFound)
	}
	return err
}

func errorsIs(err, target error) bool { return errors.Is(err, target) }

// RegisterRoutes memasang endpoint Golden_Set pada grup admin.
func (h *Handler) RegisterRoutes(admin *gin.RouterGroup) {
	admin.GET("/golden-cases", h.ListCases)
	admin.POST("/golden-cases", h.CreateCase)
	admin.PATCH("/golden-cases/:id", h.PatchCase)
	admin.DELETE("/golden-cases/:id", h.DeleteCase)
	admin.GET("/golden-runs", h.ListRuns)
	admin.POST("/golden-runs", h.CreateRun)
}
