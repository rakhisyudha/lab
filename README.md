# Backend Lab

Backend pembelajaran berbasis Go: konten bilingual, assessment, interview AI, penilaian otomatis, dan Mastery v1.

## Arsitektur Singkat

```
Client → API (Gin, /api/v1) → PostgreSQL
              ↓
         ai_tasks (antrean) → Worker → AI Provider
```

- **API** (`cmd/api`): melayani HTTP.
- **Worker** (`cmd/worker`): memproses antrean AI dan job terjadwal.
- **PostgreSQL**: satu-satunya penyimpanan. Antrean, kuota, dan rate limit memakai tabel, bukan Redis.

Logika penilaian ada di `internal/evaluation` sebagai **fungsi murni** (tanpa database/jam/acak). Server yang menentukan skor akhir dan status mastery — AI hanya memberikan sinyal.

## Menjalankan

Butuh Go 1.26+ dan PostgreSQL 13+.

```bash
# 1. Siapkan database
export DATABASE_URL='postgres://user:pass@localhost:5432/backendlab?sslmode=disable'

# 2. Migrasi + data awal
go run ./cmd/migrate up
go run ./cmd/migrate seed-prompts
go run ./cmd/migrate bootstrap-admin --email=admin@example.id   # password dari BOOTSTRAP_ADMIN_PASSWORD

# 3. Jalankan server (port default 8080, ubah dengan PORT)
export JWT_SIGNING_KEY='kunci-rahasia-minimal-32-byte'
go run ./cmd/api

# 4. Jalankan worker di terminal lain
go run ./cmd/worker
```

Variabel penting lain: `CORS_ALLOWED_ORIGINS`, `DEPLOYMENT_MODE`, `RATELIMIT_STORE`, `QUEUE_STORE`, `PORT`.
Kredensial AI provider dibaca dari environment, misalnya `PRIMARY_BASE_URL` dan `PRIMARY_API_KEY`.

## Endpoint Utama

| Grup | Contoh |
|---|---|
| Auth | `/api/v1/auth/register`, `/login`, `/refresh`, `/logout`, `/me` |
| Konten | `/api/v1/topics`, `/topics/{id}/materials`, `/materials/{id}`, `/roadmaps` |
| Assessment | `/api/v1/assessments/attempts`, `.../submit`, `.../evaluation/retry` |
| Interview | `/api/v1/interviews`, `.../start`, `.../pause`, `.../resume`, `.../answers`, `.../evaluation` |
| Progress | `/api/v1/me/progress`, `/me/misconceptions`, `/me/recommendations` |
| Admin | `/api/v1/admin/{topics,materials,questions,...}`, `/settings`, `/golden-runs` |
| Health | `/api/v1/health` |

Semua error memakai satu bentuk: `{"error":{"code","message","details","request_id"}}`.

## Pengujian

```bash
go test ./...
bash scripts/check-properties.sh          # vet + gofmt + test + cek property wajib
go test -rapid.checks=100 -run '^TestPropW' ./internal/...   # property test wajib
bash scripts/verify-migrations.sh          # butuh TEST_DATABASE_URL
```

Property test wajib menguji aturan inti: skor, mastery, state machine, antrean, dan token.

## Perilaku yang Perlu Diketahui

- **Refresh token tanpa grace window** — dua request refresh bersamaan membatalkan seluruh sesi. Klien wajib single-flight (contoh ada di `examples/web-client`).
- **Jawaban kosong** langsung dinilai skor minimum tanpa memanggil AI.
- **Mastery** hanya dari jawaban yang berstatus `scored`; kegagalan evaluasi tidak mengubah mastery.
- **Golden Set mode `recorded`** dipakai CI tanpa provider; mode `live` memanggil model nyata dan dipicu manual/terjadwal.
- Parameter (ambang, kuota, TTL) disimpan di `app_settings` dan dapat diubah Admin tanpa deploy.

## Struktur

```
cmd/api, cmd/worker, cmd/migrate   binary
internal/platform                  config, db, HTTP, pagination, settings, ratelimit
internal/{auth,content,assessment,interview,progress,recommendation}  modul bisnis
internal/ai                        provider, gateway, guard, prompt, queue, quota
internal/evaluation                fungsi murni + property test
internal/golden, internal/admin    Golden Set dan konfigurasi
migrations, prompts, test, scripts SQL, kontrak prompt, fixture, dan skrip CI
```

Spesifikasi lengkap ada di `.kiro/specs/backend-lab-technical-specs/`.
