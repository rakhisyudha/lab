package taskqueue

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/rakhisyudha/lab/internal/auth"
	"github.com/rakhisyudha/lab/internal/platform/httpx"
	"github.com/rakhisyudha/lab/internal/platform/httpx/apperr"
	"github.com/rakhisyudha/lab/internal/platform/pagination"
)

// Handler mengekspos endpoint status AITask. Payload dan result sengaja
// tidak dikirim karena dapat memuat teks jawaban Learner (Requirement 16.16).
type Handler struct {
	queue *PostgresQueue
}

func NewHandler(q *PostgresQueue) *Handler { return &Handler{queue: q} }

// TaskView adalah DTO publik status AITask.
type TaskView struct {
	ID           string `json:"id"`
	TaskType     string `json:"task_type"`
	Status       string `json:"status"`
	Priority     int    `json:"priority"`
	Attempts     int    `json:"attempts"`
	MaxAttempts  int    `json:"max_attempts"`
	ErrorCode    string `json:"error_code,omitempty"`
	RunAfter     any    `json:"run_after"`
	ResourceType string `json:"resource_type,omitempty"`
	CreatedAt    any    `json:"created_at"`
}

func view(t Task) TaskView {
	return TaskView{
		ID: t.ID.String(), TaskType: t.TaskType, Status: t.Status,
		Priority: t.Priority, Attempts: t.Attempts, MaxAttempts: t.MaxAttempts,
		ErrorCode: t.ErrorCode, RunAfter: t.RunAfter, ResourceType: t.ResourceType,
		CreatedAt: t.CreatedAt,
	}
}

func (h *Handler) Get(c *gin.Context) {
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		httpx.Fail(c, apperr.Validation(apperr.FieldError{Field: "id", Reason: "uuid"}))
		return
	}
	t, err := h.queue.Get(c.Request.Context(), id)
	if err != nil {
		httpx.Fail(c, apperr.New(apperr.CodeNotFound))
		return
	}
	p, ok := auth.PrincipalFrom(c.Request.Context())
	if !ok {
		httpx.Fail(c, apperr.New(apperr.CodeUnauthenticated))
		return
	}
	if p.Role != "admin" {
		if t.UserID == nil || t.UserID.String() != p.UserID {
			httpx.Fail(c, apperr.New(apperr.CodeNotFound))
			return
		}
	}
	c.JSON(http.StatusOK, view(t))
}

func (h *Handler) List(c *gin.Context) {
	req, err := pagination.ParseOffsetQuery(pagination.KindAdmin, c.Query("limit"), c.Query("offset"))
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	status := c.Query("status")
	taskType := c.Query("task_type")
	q := `SELECT ` + taskColumns + ` FROM ai_tasks WHERE true`
	var args []any
	if status != "" {
		q += ` AND status = $` + itoa(len(args)+1)
		args = append(args, status)
	}
	if taskType != "" {
		q += ` AND task_type = $` + itoa(len(args)+1)
		args = append(args, taskType)
	}
	q += ` ORDER BY created_at DESC, id DESC`
	rows, err := h.queue.pool.Query(c.Request.Context(), q, args...)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	var tasks []Task
	for rows.Next() {
		t, err := scanTask(rows)
		if err != nil {
			rows.Close()
			httpx.Fail(c, err)
			return
		}
		tasks = append(tasks, t)
	}
	rows.Close()
	views := make([]TaskView, 0, len(tasks))
	for _, t := range tasks {
		views = append(views, view(t))
	}
	c.JSON(http.StatusOK, pagination.NewOffsetPage(views, req, int64(len(views))))
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

// RegisterRoutes memasang endpoint status AITask.
func (h *Handler) RegisterRoutes(api *gin.RouterGroup, admin *gin.RouterGroup, authenticate gin.HandlerFunc) {
	api.GET("/ai-tasks/:id", authenticate, h.Get)
	admin.GET("/ai-tasks", h.List)
}

var _ = gin.H{}
