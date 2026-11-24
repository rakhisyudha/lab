// Package recommendation mengekspos rule engine rekomendasi dan Roadmap Learner.
package recommendation

import (
	"context"
	"fmt"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/rakhisyudha/lab/internal/auth"
	"github.com/rakhisyudha/lab/internal/evaluation"
	"github.com/rakhisyudha/lab/internal/platform/httpx"
	"github.com/rakhisyudha/lab/internal/platform/httpx/apperr"
	"github.com/rakhisyudha/lab/internal/platform/settings"
)

type Handler struct {
	pool     *pgxpool.Pool
	settings interface {
		Snapshot(ctx context.Context, keys ...string) (settings.Snapshot, error)
	}
}

func NewHandler(pool *pgxpool.Pool, s interface {
	Snapshot(ctx context.Context, keys ...string) (settings.Snapshot, error)
}) *Handler {
	return &Handler{pool: pool, settings: s}
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

func (h *Handler) Recommend(c *gin.Context) {
	uid, ok := userID(c)
	if !ok {
		return
	}
	locale := c.Query("locale")
	if locale == "" {
		locale = "id"
	}
	limit := 5
	if raw := c.Query("limit"); raw != "" {
		var n int
		if _, err := fmt.Sscanf(raw, "%d", &n); err != nil || n < 1 || n > 50 {
			httpx.Fail(c, apperr.Validation(apperr.FieldError{Field: "limit", Reason: "range_1_50"}))
			return
		}
		limit = n
	}
	topics, err := h.topics(c, locale)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	prereq, err := h.prerequisites(c)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	mastery, err := h.mastery(c, uid)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	roadmaps, err := h.roadmaps(c, uid)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	snap, err := h.settings.Snapshot(c.Request.Context(), "recommendation.low_mastery_threshold", "recommendation.limit")
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	threshold := snap.Float("recommendation.low_mastery_threshold")
	if threshold <= 0 {
		threshold = 60
	}
	if l := int(snap.Int("recommendation.limit")); l > 0 && l < limit {
		limit = l
	}
	out := evaluation.Recommend(topics, prereq, mastery, roadmaps, threshold, limit)
	c.JSON(http.StatusOK, gin.H{"items": out})
}

func (h *Handler) topics(c *gin.Context, locale string) ([]evaluation.RecommendTopic, error) {
	rows, err := h.pool.Query(c.Request.Context(), `SELECT t.id, t.slug, t.position FROM topics t
		JOIN topic_translations tr ON tr.topic_id = t.id AND tr.locale = $1 AND tr.status = 'published'
		WHERE t.deleted_at IS NULL AND t.is_active ORDER BY t.position, t.id`, locale)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]evaluation.RecommendTopic, 0)
	for rows.Next() {
		var t evaluation.RecommendTopic
		var id uuid.UUID
		if err := rows.Scan(&id, &t.Slug, &t.Position); err != nil {
			return nil, err
		}
		t.ID = id.String()
		out = append(out, t)
	}
	return out, rows.Err()
}

func (h *Handler) prerequisites(c *gin.Context) (map[string][]string, error) {
	rows, err := h.pool.Query(c.Request.Context(), `SELECT topic_id, prerequisite_topic_id FROM topic_prerequisites`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string][]string{}
	for rows.Next() {
		var topic, prereq uuid.UUID
		if err := rows.Scan(&topic, &prereq); err != nil {
			return nil, err
		}
		out[topic.String()] = append(out[topic.String()], prereq.String())
	}
	return out, rows.Err()
}

func (h *Handler) mastery(c *gin.Context, uid uuid.UUID) (map[string]evaluation.MasteryInfo, error) {
	rows, err := h.pool.Query(c.Request.Context(), `SELECT topic_id, status, avg_last3 FROM mastery_records WHERE user_id=$1`, uid)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]evaluation.MasteryInfo{}
	for rows.Next() {
		var topic uuid.UUID
		var info evaluation.MasteryInfo
		if err := rows.Scan(&topic, &info.Status, &info.AvgLast3); err != nil {
			return nil, err
		}
		out[topic.String()] = info
	}
	return out, rows.Err()
}

func (h *Handler) roadmaps(c *gin.Context, uid uuid.UUID) ([][]string, error) {
	rows, err := h.pool.Query(c.Request.Context(), `SELECT rt.roadmap_id, rt.topic_id
		FROM user_roadmaps ur JOIN roadmap_topics rt ON rt.roadmap_id = ur.roadmap_id
		WHERE ur.user_id=$1 ORDER BY ur.selected_at, rt.roadmap_id, rt.position`, uid)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	orders := map[string][]string{}
	var sequence []string
	for rows.Next() {
		var roadmap, topic uuid.UUID
		if err := rows.Scan(&roadmap, &topic); err != nil {
			return nil, err
		}
		key := roadmap.String()
		if _, ok := orders[key]; !ok {
			sequence = append(sequence, key)
		}
		orders[key] = append(orders[key], topic.String())
	}
	out := make([][]string, 0, len(sequence))
	for _, key := range sequence {
		out = append(out, orders[key])
	}
	return out, rows.Err()
}

func (h *Handler) SelectRoadmap(c *gin.Context) {
	uid, ok := userID(c)
	if !ok {
		return
	}
	roadmapID, err := uuid.Parse(c.Param("roadmap_id"))
	if err != nil {
		httpx.Fail(c, apperr.Validation(apperr.FieldError{Field: "roadmap_id", Reason: "uuid"}))
		return
	}
	if _, err := h.pool.Exec(c.Request.Context(), `INSERT INTO user_roadmaps (user_id, roadmap_id)
		VALUES ($1,$2) ON CONFLICT DO NOTHING`, uid, roadmapID); err != nil {
		httpx.Fail(c, err)
		return
	}
	c.Status(http.StatusNoContent)
}

func (h *Handler) RemoveRoadmap(c *gin.Context) {
	uid, ok := userID(c)
	if !ok {
		return
	}
	roadmapID, err := uuid.Parse(c.Param("roadmap_id"))
	if err != nil {
		httpx.Fail(c, apperr.Validation(apperr.FieldError{Field: "roadmap_id", Reason: "uuid"}))
		return
	}
	if _, err := h.pool.Exec(c.Request.Context(), `DELETE FROM user_roadmaps WHERE user_id=$1 AND roadmap_id=$2`,
		uid, roadmapID); err != nil {
		httpx.Fail(c, err)
		return
	}
	c.Status(http.StatusNoContent)
}

func (h *Handler) RegisterRoutes(me *gin.RouterGroup) {
	me.GET("/recommendations", h.Recommend)
	me.PUT("/roadmaps/:roadmap_id", h.SelectRoadmap)
	me.DELETE("/roadmaps/:roadmap_id", h.RemoveRoadmap)
}
