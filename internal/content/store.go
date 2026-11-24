package content

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/rakhisyudha/lab/internal/platform/db"
)

// Store mengakses tabel konten. Nama tabel/kolom berasal dari deskriptor
// Resource yang tetap, bukan dari input klien.
type Store struct {
	pool *pgxpool.Pool
}

func NewStore(pool *pgxpool.Pool) *Store {
	return &Store{pool: pool}
}

// ErrNotFound menandakan entitas tidak ditemukan.
var ErrNotFound = errors.New("content: tidak ditemukan")

// ErrVersionConflict menandakan version Translation sudah berubah.
var ErrVersionConflict = errors.New("content: version conflict")

// ErrPrerequisiteCycle menandakan prasyarat Topic membentuk siklus.
var ErrPrerequisiteCycle = errors.New("content: prerequisite cycle")

func withTx(ctx context.Context, pool *pgxpool.Pool, fn func(pgx.Tx) error) error {
	return db.WithTx(ctx, pool, fn)
}

func scanMap(rows pgx.Rows) (map[string]any, error) {
	fields := rows.FieldDescriptions()
	values, err := rows.Values()
	if err != nil {
		return nil, err
	}
	out := make(map[string]any, len(fields))
	for i, f := range fields {
		out[string(f.Name)] = normalizeValue(values[i])
	}
	return out, nil
}

func normalizeValue(v any) any {
	switch x := v.(type) {
	case nil, bool, string, int, int16, int32, int64, float32, float64, time.Time:
		return x
	case []byte:
		return string(x)
	case uuid.UUID:
		return x.String()
	case [16]byte:
		return uuid.UUID(x).String()
	default:
		return fmt.Sprint(x)
	}
}

func collect(rows pgx.Rows) ([]map[string]any, error) {
	defer rows.Close()
	items := make([]map[string]any, 0)
	for rows.Next() {
		item, err := scanMap(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

// SupportedLocales mengembalikan daftar Locale yang didukung dan Default_Locale
// (Requirement 5.1, 5.3).
func (s *Store) SupportedLocales(ctx context.Context) (map[string]bool, string, error) {
	rows, err := s.pool.Query(ctx, `SELECT code FROM supported_locales WHERE is_enabled`)
	if err != nil {
		return nil, "", err
	}
	defer rows.Close()
	supported := map[string]bool{}
	var fallback string
	for rows.Next() {
		var code string
		if err := rows.Scan(&code); err != nil {
			return nil, "", err
		}
		supported[code] = true
		if fallback == "" || code == "id" {
			fallback = code
		}
	}
	if err := rows.Err(); err != nil {
		return nil, "", err
	}
	var defaultLocale string
	if err := s.pool.QueryRow(ctx,
		`SELECT code FROM supported_locales WHERE is_default AND is_enabled`).Scan(&defaultLocale); err == nil {
		fallback = defaultLocale
	}
	return supported, fallback, nil
}

// FindEntity mencari satu entitas berdasarkan id atau slug.
func (s *Store) FindEntity(ctx context.Context, r Resource, key string) (map[string]any, error) {
	var args []any
	var where string
	if id, err := uuid.Parse(key); err == nil {
		args = []any{id}
		where = `id = $1`
		if r.Slug {
			where = `id = $1 OR slug = $2`
			args = append(args, key)
		}
	} else if r.Slug {
		where = `slug = $1`
		args = []any{key}
	} else {
		return nil, ErrNotFound
	}
	q := fmt.Sprintf(`SELECT * FROM %s WHERE %s`, r.Table, where)
	rows, err := s.pool.Query(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	items, err := collect(rows)
	if err != nil {
		return nil, err
	}
	if len(items) == 0 {
		return nil, ErrNotFound
	}
	return items[0], nil
}

// ListEntities mengembalikan seluruh entitas untuk keperluan admin dengan
// offset pagination.
func (s *Store) ListEntities(ctx context.Context, r Resource, limit, offset int) ([]map[string]any, int64, error) {
	var total int64
	if err := s.pool.QueryRow(ctx, fmt.Sprintf(`SELECT count(*) FROM %s`, r.Table)).Scan(&total); err != nil {
		return nil, 0, err
	}
	rows, err := s.pool.Query(ctx,
		fmt.Sprintf(`SELECT * FROM %s ORDER BY created_at DESC, id DESC LIMIT $1 OFFSET $2`, r.Table), limit, offset)
	if err != nil {
		return nil, 0, err
	}
	items, err := collect(rows)
	return items, total, err
}

// CreateEntity menyisipkan baris entitas dengan kolom yang diizinkan.
func (s *Store) CreateEntity(ctx context.Context, r Resource, fields map[string]any) (map[string]any, error) {
	id, err := uuid.NewV7()
	if err != nil {
		return nil, err
	}
	columns := []string{"id"}
	args := []any{id}
	for _, c := range r.EntityColumns {
		if v, ok := fields[c]; ok {
			columns = append(columns, c)
			args = append(args, v)
		}
	}
	params := make([]string, len(args))
	for i := range args {
		params[i] = fmt.Sprintf("$%d", i+1)
	}
	q := fmt.Sprintf(`INSERT INTO %s (%s) VALUES (%s) RETURNING *`, r.Table,
		strings.Join(columns, ", "), strings.Join(params, ", "))
	rows, err := s.pool.Query(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	items, err := collect(rows)
	if err != nil || len(items) == 0 {
		return nil, err
	}
	return items[0], nil
}

// UpdateEntity memperbarui kolom yang diizinkan pada satu entitas.
func (s *Store) UpdateEntity(ctx context.Context, r Resource, id uuid.UUID, fields map[string]any) (map[string]any, error) {
	sets := make([]string, 0, len(fields))
	args := make([]any, 0, len(fields)+1)
	idx := 1
	for _, c := range r.EntityColumns {
		if v, ok := fields[c]; ok {
			sets = append(sets, fmt.Sprintf("%s = $%d", c, idx))
			args = append(args, v)
			idx++
		}
	}
	args = append(args, id)
	if len(sets) == 0 {
		return s.entityByID(ctx, r, id)
	}
	q := fmt.Sprintf(`UPDATE %s SET %s WHERE id = $%d RETURNING *`, r.Table, strings.Join(sets, ", "), idx)
	rows, err := s.pool.Query(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	items, err := collect(rows)
	if err != nil || len(items) == 0 {
		return nil, ErrNotFound
	}
	return items[0], nil
}

func (s *Store) entityByID(ctx context.Context, r Resource, id uuid.UUID) (map[string]any, error) {
	rows, err := s.pool.Query(ctx, fmt.Sprintf(`SELECT * FROM %s WHERE id = $1`, r.Table), id)
	if err != nil {
		return nil, err
	}
	items, err := collect(rows)
	if err != nil || len(items) == 0 {
		return nil, ErrNotFound
	}
	return items[0], nil
}

// DeleteEntity melakukan soft delete bila tersedia, selain itu menonaktifkan.
func (s *Store) DeleteEntity(ctx context.Context, r Resource, id uuid.UUID) error {
	var q string
	switch {
	case r.SoftDelete:
		q = fmt.Sprintf(`UPDATE %s SET deleted_at = now() WHERE id = $1 AND deleted_at IS NULL`, r.Table)
	case r.Active:
		q = fmt.Sprintf(`UPDATE %s SET is_active = false WHERE id = $1`, r.Table)
	default:
		q = fmt.Sprintf(`DELETE FROM %s WHERE id = $1`, r.Table)
	}
	tag, err := s.pool.Exec(ctx, q, id)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// ListPublished mengembalikan entitas yang memiliki Published_Translation
// pada locale, dengan cursor keyset berdasarkan id (Requirement 5.4, 5.5).
func (s *Store) ListPublished(ctx context.Context, r Resource, locale string, parentID *uuid.UUID,
	after *uuid.UUID, limit int) ([]map[string]any, error) {
	args := []any{locale}
	next := 2
	cursorExpr := `'00000000-0000-0000-0000-000000000000'::uuid`
	if after != nil {
		cursorExpr = fmt.Sprintf("$%d::uuid", next)
		args = append(args, *after)
		next++
	}
	where := fmt.Sprintf(` AND e.id > %s`, cursorExpr)
	if r.SoftDelete {
		where += ` AND e.deleted_at IS NULL`
	}
	if r.Active {
		where += ` AND e.is_active`
	}
	if r.ParentColumn != "" && parentID != nil {
		where += fmt.Sprintf(` AND e.%s = $%d`, r.ParentColumn, next)
		args = append(args, *parentID)
		next++
	}
	q := fmt.Sprintf(`SELECT e.id FROM %s e
		JOIN %s tr ON tr.%s = e.id AND tr.locale = $1 AND tr.status = 'published'
		WHERE true%s ORDER BY e.id LIMIT $%d`, r.Table, r.TranslationTable, r.OwnerColumn, where, next)
	args = append(args, limit)

	rows, err := s.pool.Query(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	var ids []uuid.UUID
	for rows.Next() {
		var id uuid.UUID
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return nil, err
		}
		ids = append(ids, id)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return nil, err
	}
	rows.Close()
	if len(ids) == 0 {
		return []map[string]any{}, nil
	}
	return s.mergePublished(ctx, r, locale, ids)
}

// GetPublished mengambil satu entitas beserta Translation pada locale.
func (s *Store) GetPublished(ctx context.Context, r Resource, key, locale string) (map[string]any, []string, error) {
	entity, err := s.FindEntity(ctx, r, key)
	if err != nil {
		return nil, nil, err
	}
	id, err := uuid.Parse(fmt.Sprint(entity["id"]))
	if err != nil {
		return nil, nil, ErrNotFound
	}
	served, err := s.mergePublished(ctx, r, locale, []uuid.UUID{id})
	if err != nil {
		return nil, nil, err
	}
	if len(served) > 0 {
		return served[0], nil, nil
	}
	// Kumpulkan Locale yang memiliki Published_Translation (Requirement 5.6).
	rows, err := s.pool.Query(ctx, fmt.Sprintf(
		`SELECT locale FROM %s WHERE %s = $1 AND status = 'published' ORDER BY locale`,
		r.TranslationTable, r.OwnerColumn), id)
	if err != nil {
		return nil, nil, err
	}
	var locales []string
	for rows.Next() {
		var loc string
		if err := rows.Scan(&loc); err != nil {
			rows.Close()
			return nil, nil, err
		}
		locales = append(locales, loc)
	}
	rows.Close()
	return nil, locales, nil
}

func (s *Store) mergePublished(ctx context.Context, r Resource, locale string, ids []uuid.UUID) ([]map[string]any, error) {
	cols := append([]string{r.OwnerColumn}, r.TextColumns...)
	q := fmt.Sprintf(`SELECT %s FROM %s WHERE %s = ANY($1::uuid[]) AND locale = $2 AND status = 'published'`,
		strings.Join(cols, ", "), r.TranslationTable, r.OwnerColumn)
	rows, err := s.pool.Query(ctx, q, ids, locale)
	if err != nil {
		return nil, err
	}
	byID := map[string]map[string]any{}
	for rows.Next() {
		tr, err := scanMap(rows)
		if err != nil {
			rows.Close()
			return nil, err
		}
		owner := fmt.Sprint(tr[r.OwnerColumn])
		delete(tr, r.OwnerColumn)
		tr["locale"] = locale
		byID[owner] = tr
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}

	entityRows, err := s.pool.Query(ctx, fmt.Sprintf(`SELECT * FROM %s WHERE id = ANY($1::uuid[])`, r.Table), ids)
	if err != nil {
		return nil, err
	}
	entities, err := collect(entityRows)
	if err != nil {
		return nil, err
	}
	out := make([]map[string]any, 0, len(ids))
	for _, e := range entities {
		tr, ok := byID[fmt.Sprint(e["id"])]
		if !ok {
			continue
		}
		merged := map[string]any{}
		for k, v := range e {
			merged[k] = v
		}
		for k, v := range tr {
			merged[k] = v
		}
		out = append(out, merged)
	}
	return out, nil
}

// FindTranslation mengambil Translation berdasarkan owner dan locale.
func (s *Store) FindTranslation(ctx context.Context, r Resource, ownerID uuid.UUID, locale string) (map[string]any, error) {
	cols := append([]string{r.OwnerColumn, "status", "version", "reviewer_id", "published_at"}, r.TextColumns...)
	rows, err := s.pool.Query(ctx,
		fmt.Sprintf(`SELECT %s FROM %s WHERE %s = $1 AND locale = $2`,
			strings.Join(cols, ", "), r.TranslationTable, r.OwnerColumn), ownerID, locale)
	if err != nil {
		return nil, err
	}
	items, err := collect(rows)
	if err != nil {
		return nil, err
	}
	if len(items) == 0 {
		return nil, ErrNotFound
	}
	return items[0], nil
}

// UpsertTranslation menyimpan atau memperbarui Translation dengan version
// optimistic. Mengembalikan ErrVersionConflict bila version tidak cocok.
func (s *Store) UpsertTranslation(ctx context.Context, r Resource, ownerID uuid.UUID, locale string,
	text map[string]any, expectedVersion int, next TranslationState) error {
	existing, err := s.FindTranslation(ctx, r, ownerID, locale)
	if errors.Is(err, ErrNotFound) {
		cols := []string{"id", r.OwnerColumn, "locale", "status", "version"}
		vals := []any{uuid.Must(uuid.NewV7()), ownerID, locale, StatusDraft, 1}
		for _, c := range r.TextColumns {
			cols = append(cols, c)
			vals = append(vals, text[c])
		}
		params := make([]string, len(vals))
		for i := range vals {
			params[i] = fmt.Sprintf("$%d", i+1)
		}
		_, err = s.pool.Exec(ctx, fmt.Sprintf(`INSERT INTO %s (%s) VALUES (%s)`,
			r.TranslationTable, strings.Join(cols, ", "), strings.Join(params, ", ")), vals...)
		return err
	}
	if err != nil {
		return err
	}
	if expectedVersion != 0 && fmt.Sprint(existing["version"]) != fmt.Sprint(expectedVersion) {
		return ErrVersionConflict
	}
	sets := make([]string, 0, len(r.TextColumns)+2)
	args := make([]any, 0, len(r.TextColumns)+4)
	idx := 1
	for _, c := range r.TextColumns {
		sets = append(sets, fmt.Sprintf("%s = $%d", c, idx))
		args = append(args, text[c])
		idx++
	}
	sets = append(sets, fmt.Sprintf("version = version + 1"), fmt.Sprintf("status = $%d", idx))
	args = append(args, next.Status)
	idx++
	if next.Status == StatusPublished {
		sets = append(sets, fmt.Sprintf("reviewer_id = $%d", idx), fmt.Sprintf("reviewed_at = $%d", idx+1), fmt.Sprintf("published_at = $%d", idx+2))
		args = append(args, next.ReviewerID, next.ReviewedAt, next.PublishedAt)
		idx += 3
	} else {
		sets = append(sets, fmt.Sprintf("published_at = $%d", idx))
		args = append(args, nil)
		idx++
	}
	args = append(args, ownerID, locale)
	q := fmt.Sprintf(`UPDATE %s SET %s WHERE %s = $%d AND locale = $%d`,
		r.TranslationTable, strings.Join(sets, ", "), r.OwnerColumn, idx, idx+1)
	tag, err := s.pool.Exec(ctx, q, args...)
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23514" {
			return ErrVersionConflict
		}
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrVersionConflict
	}
	return nil
}

// SetTranslationStatus menerapkan hasil transisi status tanpa mengubah version
// ataupun field teks.
func (s *Store) SetTranslationStatus(ctx context.Context, r Resource, ownerID uuid.UUID,
	locale string, next TranslationState) error {
	sets := []string{"status = $3"}
	args := []any{ownerID, locale, next.Status}
	if next.Status == StatusPublished {
		sets = append(sets, "reviewer_id = $4", "reviewed_at = $5", "published_at = $6")
		args = append(args, next.ReviewerID, next.ReviewedAt, next.PublishedAt)
	} else {
		sets = append(sets, "published_at = NULL")
	}
	q := fmt.Sprintf(`UPDATE %s SET %s WHERE %s = $1 AND locale = $2`,
		r.TranslationTable, strings.Join(sets, ", "), r.OwnerColumn)
	tag, err := s.pool.Exec(ctx, q, args...)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// QuestionCanPublish memeriksa syarat publikasi Question (Requirement 20.4).
func (s *Store) QuestionCanPublish(ctx context.Context, questionID uuid.UUID) (bool, error) {
	var concepts int
	if err := s.pool.QueryRow(ctx, `SELECT count(*) FROM question_concepts WHERE question_id = $1`, questionID).Scan(&concepts); err != nil {
		return false, err
	}
	var reference string
	if err := s.pool.QueryRow(ctx,
		`SELECT reference_answer FROM question_translations WHERE question_id = $1 AND status = 'published'`,
		questionID).Scan(&reference); err != nil {
		return false, err
	}
	return concepts > 0 && strings.TrimSpace(reference) != "", nil
}

// replaceQuestionConcepts mengganti relasi Question-Concept dalam satu transaksi.
func (s *Store) replaceQuestionConcepts(ctx context.Context, questionID uuid.UUID, concepts []weightedConcept) error {
	return withTx(ctx, s.pool, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `DELETE FROM question_concepts WHERE question_id = $1`, questionID); err != nil {
			return err
		}
		for _, c := range concepts {
			id, err := uuid.Parse(c.ConceptID)
			if err != nil {
				return ErrNotFound
			}
			weight := c.Weight
			if weight == "" {
				weight = "1"
			}
			if _, err := tx.Exec(ctx, `INSERT INTO question_concepts
				(question_id, concept_id, weight, is_required) VALUES ($1, $2, $3, $4)`,
				questionID, id, weight, c.IsRequired); err != nil {
				return err
			}
		}
		return nil
	})
}

// replaceRoadmapTopics mengganti urutan Topic pada Roadmap dalam satu transaksi.
func (s *Store) replaceRoadmapTopics(ctx context.Context, roadmapID uuid.UUID, topicIDs []uuid.UUID) error {
	return withTx(ctx, s.pool, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `DELETE FROM roadmap_topics WHERE roadmap_id = $1`, roadmapID); err != nil {
			return err
		}
		for i, id := range topicIDs {
			if _, err := tx.Exec(ctx, `INSERT INTO roadmap_topics (roadmap_id, topic_id, position) VALUES ($1, $2, $3)`,
				roadmapID, id, i); err != nil {
				return err
			}
		}
		return nil
	})
}

// ListPrerequisites mengembalikan pasangan (topic, prasyarat) untuk deteksi siklus.
func (s *Store) ListPrerequisites(ctx context.Context) (map[uuid.UUID][]uuid.UUID, error) {
	rows, err := s.pool.Query(ctx, `SELECT topic_id, prerequisite_topic_id FROM topic_prerequisites`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[uuid.UUID][]uuid.UUID{}
	for rows.Next() {
		var topic, prereq uuid.UUID
		if err := rows.Scan(&topic, &prereq); err != nil {
			return nil, err
		}
		out[topic] = append(out[topic], prereq)
	}
	return out, rows.Err()
}

// SetPrerequisites mengganti seluruh prasyarat sebuah Topic dalam satu transaksi.
func (s *Store) SetPrerequisites(ctx context.Context, topicID uuid.UUID, prerequisites []uuid.UUID, valid func(map[uuid.UUID][]uuid.UUID) bool) error {
	return withTx(ctx, s.pool, func(tx pgx.Tx) error {
		rows, err := tx.Query(ctx, `SELECT topic_id, prerequisite_topic_id FROM topic_prerequisites`)
		if err != nil {
			return err
		}
		graph := map[uuid.UUID][]uuid.UUID{}
		for rows.Next() {
			var t, p uuid.UUID
			if err := rows.Scan(&t, &p); err != nil {
				rows.Close()
				return err
			}
			graph[t] = append(graph[t], p)
		}
		rows.Close()
		graph[topicID] = prerequisites
		if !valid(graph) {
			return ErrPrerequisiteCycle
		}
		if _, err := tx.Exec(ctx, `DELETE FROM topic_prerequisites WHERE topic_id = $1`, topicID); err != nil {
			return err
		}
		for _, p := range prerequisites {
			if _, err := tx.Exec(ctx,
				`INSERT INTO topic_prerequisites (topic_id, prerequisite_topic_id) VALUES ($1, $2)`, topicID, p); err != nil {
				return err
			}
		}
		return nil
	})
}
