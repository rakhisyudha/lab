// Package admin mengekspos endpoint konfigurasi AI, Prompt_Contract, dan App_Settings.
package admin

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/rakhisyudha/lab/internal/auth"
	"github.com/rakhisyudha/lab/internal/platform/db"
	"github.com/rakhisyudha/lab/internal/platform/httpx"
	"github.com/rakhisyudha/lab/internal/platform/httpx/apperr"
	"github.com/rakhisyudha/lab/internal/platform/pagination"
	"github.com/rakhisyudha/lab/internal/platform/settings"
)

// Handler menangani konfigurasi AI.
type Handler struct {
	pool     *pgxpool.Pool
	settings *settings.Service
}

func NewHandler(pool *pgxpool.Pool, s *settings.Service) *Handler {
	return &Handler{pool: pool, settings: s}
}

// ProviderView adalah DTO ai_provider_settings.
type ProviderView struct {
	ID            string `json:"id"`
	ProviderName  string `json:"provider_name"`
	ModelName     string `json:"model_name"`
	FallbackOrder int    `json:"fallback_order"`
	RPM           int    `json:"rpm_limit"`
	TPM           int    `json:"tpm_limit"`
	IsEnabled     bool   `json:"is_enabled"`
}

func (h *Handler) ListProviders(c *gin.Context) {
	rows, err := h.pool.Query(c.Request.Context(), `SELECT id, provider_name, model_name,
		fallback_order, rpm_limit, tpm_limit, is_enabled FROM ai_provider_settings ORDER BY fallback_order`)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	defer rows.Close()
	items := make([]ProviderView, 0)
	for rows.Next() {
		var v ProviderView
		var id uuid.UUID
		if err := rows.Scan(&id, &v.ProviderName, &v.ModelName, &v.FallbackOrder, &v.RPM, &v.TPM, &v.IsEnabled); err != nil {
			httpx.Fail(c, err)
			return
		}
		v.ID = id.String()
		items = append(items, v)
	}
	c.JSON(http.StatusOK, gin.H{"items": items})
}

type providerRequest struct {
	ProviderName  string `json:"provider_name" binding:"required"`
	ModelName     string `json:"model_name" binding:"required"`
	FallbackOrder int    `json:"fallback_order" binding:"required,min=1"`
	RPM           int    `json:"rpm_limit" binding:"required,min=1"`
	TPM           int    `json:"tpm_limit" binding:"required,min=1"`
	IsEnabled     bool   `json:"is_enabled"`
}

func (h *Handler) PutProvider(c *gin.Context) {
	var req providerRequest
	if err := httpx.BindJSON(c, &req); err != nil {
		httpx.Fail(c, err)
		return
	}
	_, err := h.pool.Exec(c.Request.Context(), `INSERT INTO ai_provider_settings
		(provider_name, model_name, fallback_order, rpm_limit, tpm_limit, is_enabled)
		VALUES ($1,$2,$3,$4,$5,$6)
		ON CONFLICT (provider_name, model_name) DO UPDATE SET
		  fallback_order = EXCLUDED.fallback_order, rpm_limit = EXCLUDED.rpm_limit,
		  tpm_limit = EXCLUDED.tpm_limit, is_enabled = EXCLUDED.is_enabled`,
		req.ProviderName, req.ModelName, req.FallbackOrder, req.RPM, req.TPM, req.IsEnabled)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	c.JSON(http.StatusOK, req)
}

// SettingItem adalah DTO App_Settings (tanpa value_schema mentah).
type SettingItem struct {
	Key         string          `json:"key"`
	Value       json.RawMessage `json:"value"`
	Description string          `json:"description"`
	IsInitial   bool            `json:"is_initial_value"`
	UpdatedBy   *string         `json:"updated_by"`
	UpdatedAt   time.Time       `json:"updated_at"`
}

func (h *Handler) ListSettings(c *gin.Context) {
	req, err := pagination.ParseOffsetQuery(pagination.KindAdmin, c.Query("limit"), c.Query("offset"))
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	prefix := c.Query("prefix")
	items, total, err := h.listSettings(c.Request.Context(), prefix, req.Limit, req.Offset)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	c.JSON(http.StatusOK, pagination.NewOffsetPage(items, req, total))
}

func (h *Handler) listSettings(ctx context.Context, prefix string, limit, offset int) ([]SettingItem, int64, error) {
	where, args := ``, []any{}
	if prefix != "" {
		where = ` WHERE key LIKE $1`
		args = append(args, prefix+"%")
	}
	var total int64
	if err := h.pool.QueryRow(ctx, `SELECT count(*) FROM app_settings`+where, args...).Scan(&total); err != nil {
		return nil, 0, err
	}
	q := `SELECT key, value, description, is_initial_value, updated_by, updated_at FROM app_settings` + where +
		` ORDER BY key LIMIT $` + itoa(len(args)+1) + ` OFFSET $` + itoa(len(args)+2)
	args = append(args, limit, offset)
	rows, err := h.pool.Query(ctx, q, args...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	items := make([]SettingItem, 0)
	for rows.Next() {
		var v SettingItem
		var value []byte
		var updatedBy sql.NullString
		if err := rows.Scan(&v.Key, &value, &v.Description, &v.IsInitial, &updatedBy, &v.UpdatedAt); err != nil {
			return nil, 0, err
		}
		v.Value = json.RawMessage(value)
		if updatedBy.Valid {
			v.UpdatedBy = &updatedBy.String
		}
		items = append(items, v)
	}
	return items, total, rows.Err()
}

type settingRequest struct {
	Value json.RawMessage `json:"value" binding:"required"`
}

func (h *Handler) PutSetting(c *gin.Context) {
	key := c.Param("key")
	var req settingRequest
	if err := httpx.BindJSON(c, &req); err != nil {
		httpx.Fail(c, err)
		return
	}
	p, _ := ginContextPrincipal(c)
	actor, err := uuid.Parse(p)
	if err != nil {
		httpx.Fail(c, apperr.New(apperr.CodeUnauthenticated))
		return
	}
	if err := h.settings.Update(c.Request.Context(), actor, key, req.Value); err != nil {
		httpx.Fail(c, err)
		return
	}
	c.Status(http.StatusNoContent)
}

// QuotaView adalah tampilan kuota AI tanpa tabel sendiri.
func (h *Handler) ListQuotas(c *gin.Context) {
	items, _, err := h.listSettings(c.Request.Context(), "ai.quota.", 100, 0)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"items": items})
}

func (h *Handler) PutQuota(c *gin.Context) {
	key := c.Param("key")
	if len(key) < 10 || key[:10] != "ai.quota." {
		key = "ai.quota." + key
	}
	var req settingRequest
	if err := httpx.BindJSON(c, &req); err != nil {
		httpx.Fail(c, err)
		return
	}
	p, _ := ginContextPrincipal(c)
	actor, err := uuid.Parse(p)
	if err != nil {
		httpx.Fail(c, apperr.New(apperr.CodeUnauthenticated))
		return
	}
	if err := h.settings.Update(c.Request.Context(), actor, key, req.Value); err != nil {
		httpx.Fail(c, err)
		return
	}
	c.Status(http.StatusNoContent)
}

// ContractView adalah DTO Prompt_Contract.
type ContractView struct {
	ID         string          `json:"id"`
	ContractID string          `json:"contract_id"`
	TaskType   string          `json:"task_type"`
	Version    int             `json:"version"`
	Status     string          `json:"status"`
	Parameters json.RawMessage `json:"parameters"`
}

func (h *Handler) ListContracts(c *gin.Context) {
	rows, err := h.pool.Query(c.Request.Context(), `SELECT id, contract_id, task_type, version, status, parameters
		FROM prompt_contracts ORDER BY task_type, version DESC`)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	defer rows.Close()
	items := make([]ContractView, 0)
	for rows.Next() {
		var v ContractView
		var id uuid.UUID
		var params []byte
		if err := rows.Scan(&id, &v.ContractID, &v.TaskType, &v.Version, &v.Status, &params); err != nil {
			httpx.Fail(c, err)
			return
		}
		v.ID = id.String()
		v.Parameters = json.RawMessage(params)
		items = append(items, v)
	}
	c.JSON(http.StatusOK, gin.H{"items": items})
}

type contractRequest struct {
	TaskType           string          `json:"task_type" binding:"required"`
	Version            int             `json:"version" binding:"required,min=1"`
	LocalePolicy       string          `json:"locale_policy"`
	SystemPrompt       string          `json:"system_prompt" binding:"required"`
	UserPromptTemplate string          `json:"user_prompt_template" binding:"required"`
	InputSchema        json.RawMessage `json:"input_schema"`
	OutputSchema       json.RawMessage `json:"output_schema"`
	Parameters         json.RawMessage `json:"parameters"`
}

func (h *Handler) CreateContract(c *gin.Context) {
	var req contractRequest
	if err := httpx.BindJSON(c, &req); err != nil {
		httpx.Fail(c, err)
		return
	}
	if req.LocalePolicy == "" {
		req.LocalePolicy = "session_locale"
	}
	if len(req.InputSchema) == 0 {
		req.InputSchema = json.RawMessage(`{"type":"object"}`)
	}
	if len(req.OutputSchema) == 0 {
		req.OutputSchema = json.RawMessage(`{"type":"object"}`)
	}
	if len(req.Parameters) == 0 {
		req.Parameters = json.RawMessage(`{}`)
	}
	p, _ := ginContextPrincipal(c)
	creator, _ := uuid.Parse(p)
	id, err := uuid.NewV7()
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	sum := sha256Of(req)
	_, err = h.pool.Exec(c.Request.Context(), `INSERT INTO prompt_contracts
		(id, task_type, version, locale_policy, system_prompt, user_prompt_template,
		 input_schema, output_schema, parameters, status, content_sha256, created_by)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,'draft',$10,$11)`,
		id, req.TaskType, req.Version, req.LocalePolicy, req.SystemPrompt, req.UserPromptTemplate,
		req.InputSchema, req.OutputSchema, req.Parameters, sum, creator)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	c.JSON(http.StatusCreated, gin.H{"id": id.String()})
}

type activateRequest struct {
	WaiverReason string `json:"waiver_reason"`
}

// ActivateContract menerapkan gerbang Golden_Set untuk answer_evaluation.
func (h *Handler) ActivateContract(c *gin.Context) {
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		httpx.Fail(c, apperr.Validation(apperr.FieldError{Field: "id", Reason: "uuid"}))
		return
	}
	var req activateRequest
	if err := httpx.BindJSON(c, &req); err != nil && !errors.Is(err, context.Canceled) {
		// Body opsional.
		req = activateRequest{}
	}
	var taskType string
	var status string
	if err := h.pool.QueryRow(c.Request.Context(),
		`SELECT task_type, status FROM prompt_contracts WHERE id = $1`, id).Scan(&taskType, &status); err != nil {
		httpx.Fail(c, apperr.New(apperr.CodeNotFound))
		return
	}
	if status != "draft" {
		httpx.Fail(c, apperr.New(apperr.CodeConflict))
		return
	}
	if taskType == "answer_evaluation" && req.WaiverReason == "" {
		var runMode, runStatus string
		err := h.pool.QueryRow(c.Request.Context(), `SELECT run_mode, status FROM golden_runs
			WHERE prompt_contract_id = $1 ORDER BY started_at DESC LIMIT 1`, id).Scan(&runMode, &runStatus)
		if errors.Is(err, pgx.ErrNoRows) || runMode != "live" || runStatus != "passed" {
			details := map[string]any{"reason": "run_not_live"}
			httpx.Fail(c, apperr.WithDetails(apperr.CodeGoldenGateFailed, details))
			return
		}
	}
	now := time.Now().UTC()
	err = db.WithTx(c.Request.Context(), h.pool, func(tx pgx.Tx) error {
		if _, err := tx.Exec(c.Request.Context(), `UPDATE prompt_contracts
			SET status='retired' WHERE task_type = $1 AND status='active'`, taskType); err != nil {
			return err
		}
		_, err := tx.Exec(c.Request.Context(), `UPDATE prompt_contracts
			SET status='active', activated_at=$2, activation_waiver_reason=$3 WHERE id=$1`,
			id, now, nullableString(req.WaiverReason))
		return err
	})
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	c.Status(http.StatusNoContent)
}

func nullableString(s string) any {
	if s == "" {
		return nil
	}
	return s
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var buf [20]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	return string(buf[i:])
}

func sha256Of(v contractRequest) string {
	raw, _ := json.Marshal(v)
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}

func ginContextPrincipal(c *gin.Context) (string, bool) {
	p, ok := auth.PrincipalFrom(c.Request.Context())
	if !ok {
		return "", false
	}
	return p.UserID, true
}

// RegisterRoutes memasang endpoint pada grup admin (sudah ber-RBAC).
func (h *Handler) RegisterRoutes(admin *gin.RouterGroup) {
	admin.GET("/ai-provider-settings", h.ListProviders)
	admin.PUT("/ai-provider-settings", h.PutProvider)
	admin.GET("/settings", h.ListSettings)
	admin.PUT("/settings/:key", h.PutSetting)
	admin.GET("/ai-quota-settings", h.ListQuotas)
	admin.PUT("/ai-quota-settings/:key", h.PutQuota)
	admin.GET("/prompt-contracts", h.ListContracts)
	admin.POST("/prompt-contracts", h.CreateContract)
	admin.POST("/prompt-contracts/:id/activate", h.ActivateContract)
}
