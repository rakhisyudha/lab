package main

import (
	"bytes"
	"context"
	"errors"
	"io/fs"
	"os"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/rakhisyudha/lab/migrations"
)

var migrationFileRe = regexp.MustCompile(`^\d{4}_[a-z0-9_]+\.(up|down)\.sql$`)

func TestPgx5URL(t *testing.T) {
	tests := []struct {
		name    string
		raw     string
		want    string
		wantErr bool
	}{
		{"postgres dengan sslmode", "postgres://u:p@localhost:5432/lab?sslmode=disable", "pgx5://u:p@localhost:5432/lab?sslmode=disable", false},
		{"postgresql", "postgresql://u@db.internal/lab", "pgx5://u@db.internal/lab", false},
		{"kosong", "", "", true},
		{"skema mysql", "mysql://u:p@h/db", "", true},
		{"host kosong", "postgres:///lab", "", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := pgx5URL(tt.raw)
			if (err != nil) != tt.wantErr {
				t.Fatalf("pgx5URL() error = %v, wantErr %v", err, tt.wantErr)
			}
			if got != tt.want {
				t.Fatalf("pgx5URL() = %q, want %q", got, tt.want)
			}
		})
	}
}

// Pesan error tidak boleh memuat kata sandi dari URL.
func TestPgx5URLErrorDoesNotLeakPassword(t *testing.T) {
	const secret = "rahasia-sangat-panjang-123"
	_, err := pgx5URL("mysql://user:" + secret + "@host/db")
	if err == nil {
		t.Fatal("diharapkan error untuk skema mysql")
	}
	if strings.Contains(err.Error(), secret) {
		t.Fatalf("pesan error memuat kata sandi: %v", err)
	}
}

func TestIsP1Migration(t *testing.T) {
	tests := map[string]bool{
		"0001_helpers.up.sql":          false,
		"0009_seed_reference.down.sql": false,
		"0100_p1_pgvector_rag.up.sql":  true,
		"0101_future.up.sql":           true,
		"tanpa_nomor.up.sql":           false,
		"sesuatu.sql":                  false,
	}
	for name, want := range tests {
		if got := isP1Migration(name); got != want {
			t.Errorf("isP1Migration(%q) = %v, want %v", name, got, want)
		}
	}
}

func TestFilterFSHidesP1WhenNotIncluded(t *testing.T) {
	src := fstest.MapFS{
		"0001_helpers.up.sql":         {Data: []byte("SELECT 1;")},
		"0100_p1_pgvector_rag.up.sql": {Data: []byte("CREATE EXTENSION vector;")},
	}
	list := func(includeP1 bool) []string {
		f := filterFS{fsys: src, include: func(name string) bool { return includeP1 || !isP1Migration(name) }}
		entries, err := f.ReadDir(".")
		if err != nil {
			t.Fatalf("ReadDir: %v", err)
		}
		var names []string
		for _, e := range entries {
			names = append(names, e.Name())
		}
		return names
	}

	if got := list(false); len(got) != 1 || got[0] != "0001_helpers.up.sql" {
		t.Fatalf("tanpa include-p1: %v", got)
	}
	if got := list(true); len(got) != 2 {
		t.Fatalf("dengan include-p1: %v, want 2 berkas", got)
	}
}

// Berkas P0 harus berpasangan up/down, bernama sesuai pola, dan tidak bergantung
// pada pgvector atau Redis (Requirement 2.7).
func TestEmbeddedMigrationFilesAreWellFormed(t *testing.T) {
	entries, err := fs.ReadDir(migrations.FS, ".")
	if err != nil {
		t.Fatalf("membaca embed: %v", err)
	}

	up := map[string]bool{}
	down := map[string]bool{}
	for _, e := range entries {
		name := e.Name()
		if !migrationFileRe.MatchString(name) {
			t.Errorf("nama berkas %q tidak sesuai pola NNNN_nama.(up|down).sql", name)
			continue
		}
		base, isUp := strings.CutSuffix(name, ".up.sql")
		if !isUp {
			base, _ = strings.CutSuffix(name, ".down.sql")
			down[base] = true
		} else {
			up[base] = true
		}

		if isP1Migration(name) {
			continue
		}
		data, err := fs.ReadFile(migrations.FS, name)
		if err != nil {
			t.Fatalf("membaca %s: %v", name, err)
		}
		lower := strings.ToLower(string(data))
		if strings.Contains(lower, "create extension") || strings.Contains(lower, "pgvector") || strings.Contains(lower, "redis") {
			t.Errorf("migrasi P0 %s bergantung pada ekstensi atau Redis", name)
		}
	}
	for base := range up {
		if !down[base] {
			t.Errorf("migrasi %s tidak memiliki berkas .down.sql", base)
		}
	}
	for base := range down {
		if !up[base] {
			t.Errorf("migrasi %s tidak memiliki berkas .up.sql", base)
		}
	}
	if len(up) == 0 {
		t.Fatal("tidak ada migrasi ter-embed")
	}
}

func TestRunRejectsUnknownAndIncompleteCommands(t *testing.T) {
	noEnv := func(string) (string, bool) { return "", false }
	tests := []struct {
		name string
		args []string
	}{
		{"tanpa perintah", nil},
		{"perintah tidak dikenal", []string{"seed-everything"}},
		{"down tanpa --all", []string{"down"}},
		{"flag tidak dikenal", []string{"up", "--bukan-flag"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var out, errOut bytes.Buffer
			if err := run(tt.args, &out, &errOut, noEnv); err == nil {
				t.Fatal("diharapkan error")
			}
		})
	}
}

func TestRunRequiresDatabaseURL(t *testing.T) {
	noEnv := func(string) (string, bool) { return "", false }
	var out, errOut bytes.Buffer
	err := run([]string{"up"}, &out, &errOut, noEnv)
	if err == nil || !strings.Contains(err.Error(), envDatabaseURL) {
		t.Fatalf("diharapkan error DATABASE_URL, dapat %v", err)
	}
}

// TestMigrateUpDownIntegration menjalankan up, down, lalu up lagi pada PostgreSQL
// sungguhan. Dilewati bila TEST_DATABASE_URL tidak diset. Database tersebut harus
// database uji khusus, karena `down --all` menghapus tabel migrasi.
func TestMigrateUpDownIntegration(t *testing.T) {
	testURL := os.Getenv("TEST_DATABASE_URL")
	if testURL == "" {
		t.Skip("TEST_DATABASE_URL tidak diset, test integrasi migrasi dilewati")
	}
	lookup := func(key string) (string, bool) {
		if key == envDatabaseURL {
			return testURL, true
		}
		return "", false
	}
	ctx := context.Background()
	conn, err := pgx.Connect(ctx, testURL)
	if err != nil {
		t.Fatalf("koneksi test: %v", err)
	}
	defer conn.Close(ctx)

	var out bytes.Buffer
	if err := run([]string{"down", "--all"}, &out, &out, lookup); err != nil {
		t.Fatalf("down awal: %v (output: %s)", err, out.String())
	}

	out.Reset()
	if err := run([]string{"up"}, &out, &out, lookup); err != nil {
		t.Fatalf("up: %v (output: %s)", err, out.String())
	}
	assertTablesExist(t, conn, true)
	assertSchemaVersion(t, conn, "2")

	// Requirement 2.2 dan 7.1: email unik tidak sensitif huruf besar-kecil.
	_, err = conn.Exec(ctx, `INSERT INTO users (email, password_hash) VALUES ('Learner@Example.id', 'h')`)
	if err != nil {
		t.Fatalf("insert user pertama: %v", err)
	}
	_, err = conn.Exec(ctx, `INSERT INTO users (email, password_hash) VALUES ('learner@example.id', 'h')`)
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) || pgErr.Code != "23505" {
		t.Fatalf("email beda huruf besar-kecil diharapkan unique violation 23505, dapat %v", err)
	}
	_, _ = conn.Exec(ctx, `DELETE FROM users WHERE lower(email) = 'learner@example.id'`)

	out.Reset()
	if err := run([]string{"down", "--all"}, &out, &out, lookup); err != nil {
		t.Fatalf("down: %v (output: %s)", err, out.String())
	}
	assertTablesExist(t, conn, false)

	out.Reset()
	if err := run([]string{"up"}, &out, &out, lookup); err != nil {
		t.Fatalf("up ulang: %v (output: %s)", err, out.String())
	}
	assertTablesExist(t, conn, true)
}

func assertTablesExist(t *testing.T, conn *pgx.Conn, want bool) {
	t.Helper()
	for _, table := range []string{"supported_locales", "users", "invitations", "refresh_tokens"} {
		var exists bool
		if err := conn.QueryRow(context.Background(),
			`SELECT to_regclass($1) IS NOT NULL`, "public."+table).Scan(&exists); err != nil {
			t.Fatalf("cek tabel %s: %v", table, err)
		}
		if exists != want {
			t.Fatalf("tabel %s exists=%v, want %v", table, exists, want)
		}
	}
	for _, fn := range []string{"set_updated_at()", "attach_updated_at_trigger(regclass)", "enforce_reviewer_admin()", "attach_translation_triggers(regclass)"} {
		var exists bool
		if err := conn.QueryRow(context.Background(),
			`SELECT to_regprocedure($1) IS NOT NULL`, fn).Scan(&exists); err != nil {
			t.Fatalf("cek fungsi %s: %v", fn, err)
		}
		if exists != want {
			t.Fatalf("fungsi %s exists=%v, want %v", fn, exists, want)
		}
	}
}

func assertSchemaVersion(t *testing.T, conn *pgx.Conn, want string) {
	t.Helper()
	var version int64
	var dirty bool
	if err := conn.QueryRow(context.Background(),
		`SELECT version, dirty FROM schema_migrations`).Scan(&version, &dirty); err != nil {
		t.Fatalf("membaca schema_migrations: %v", err)
	}
	if strconv.FormatInt(version, 10) != want || dirty {
		t.Fatalf("schema_migrations version=%d dirty=%v, want version=%s dirty=false", version, dirty, want)
	}
}
