// Package migrations memuat berkas SQL migrasi (golang-migrate) ke dalam binary.
//
// Setiap migrasi berbentuk pasangan NNNN_nama.up.sql dan NNNN_nama.down.sql.
// Migrasi bernomor 0100 ke atas adalah P1 dan hanya dijalankan dengan
// `cmd/migrate up --include-p1`.
package migrations

import "embed"

// FS berisi seluruh berkas .sql di direktori ini.
//
//go:embed *.sql
var FS embed.FS
