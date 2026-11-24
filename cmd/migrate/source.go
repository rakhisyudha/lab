package main

import (
	"fmt"
	"io"
	"io/fs"
	"net/url"
	"strconv"
	"strings"

	"github.com/golang-migrate/migrate/v4"
	"github.com/golang-migrate/migrate/v4/source/iofs"

	"github.com/rakhisyudha/lab/migrations"
)

const (
	envDatabaseURL = "DATABASE_URL"

	// p1FirstVersion adalah nomor migrasi pertama milik fitur P1 (RAG/pgvector).
	p1FirstVersion = 100
)

// openMigrator membuka golang-migrate dengan sumber embed dan driver pgx/v5.
// Pesan error tidak memuat URL database.
func openMigrator(databaseURL string, includeP1 bool, logOut io.Writer) (*migrate.Migrate, error) {
	dbURL, err := pgx5URL(databaseURL)
	if err != nil {
		return nil, err
	}

	src, err := iofs.New(filterFS{
		fsys:    migrations.FS,
		include: func(name string) bool { return includeP1 || !isP1Migration(name) },
	}, ".")
	if err != nil {
		return nil, fmt.Errorf("membaca berkas migrasi: %w", err)
	}

	m, err := migrate.NewWithSourceInstance("iofs", src, dbURL)
	if err != nil {
		return nil, fmt.Errorf("membuka koneksi migrasi: %w", err)
	}
	m.Log = logger{w: logOut}
	return m, nil
}

// pgx5URL mengubah DATABASE_URL (skema postgres atau postgresql) menjadi skema
// pgx5 yang dibaca driver golang-migrate. Pesan error tidak memuat URL.
func pgx5URL(raw string) (string, error) {
	if raw == "" {
		return "", fmt.Errorf("%s: wajib diisi", envDatabaseURL)
	}
	u, err := url.Parse(raw)
	if err != nil {
		return "", fmt.Errorf("%s: format URL tidak valid", envDatabaseURL)
	}
	if u.Scheme != "postgres" && u.Scheme != "postgresql" {
		return "", fmt.Errorf("%s: skema harus postgres:// atau postgresql://", envDatabaseURL)
	}
	if u.Host == "" {
		return "", fmt.Errorf("%s: host database kosong", envDatabaseURL)
	}
	u.Scheme = "pgx5"
	return u.String(), nil
}

// isP1Migration melaporkan apakah nama berkas (contoh "0100_p1_pgvector_rag.up.sql")
// berasal dari rentang P1.
func isP1Migration(name string) bool {
	prefix, _, found := strings.Cut(name, "_")
	if !found {
		return false
	}
	n, err := strconv.Atoi(prefix)
	return err == nil && n >= p1FirstVersion
}

// filterFS menyembunyikan berkas yang tidak lolos include dari daftar direktori.
// iofs hanya membaca daftar lewat ReadDir, sehingga berkas yang disaring tidak
// pernah dimuat sebagai migrasi.
type filterFS struct {
	fsys    fs.FS
	include func(name string) bool
}

// Open meneruskan ke fsys tanpa penyaringan.
func (f filterFS) Open(name string) (fs.File, error) {
	return f.fsys.Open(name)
}

// ReadDir mengembalikan entri yang lolos include.
func (f filterFS) ReadDir(name string) ([]fs.DirEntry, error) {
	entries, err := fs.ReadDir(f.fsys, name)
	if err != nil {
		return nil, err
	}
	kept := make([]fs.DirEntry, 0, len(entries))
	for _, e := range entries {
		if e.IsDir() || f.include(e.Name()) {
			kept = append(kept, e)
		}
	}
	return kept, nil
}

// logger meneruskan log golang-migrate ke stream yang diberikan.
type logger struct {
	w io.Writer
}

// Printf menulis satu pesan log.
func (l logger) Printf(format string, v ...interface{}) {
	fmt.Fprintf(l.w, format, v...)
}

// Verbose mengaktifkan log rinci golang-migrate.
func (l logger) Verbose() bool { return false }
