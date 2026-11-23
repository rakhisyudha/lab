# Implementation Plan: Backend Lab Technical Specs

## Overview

Rencana ini menerjemahkan design (modular monolith Go dengan Gin, `pgx/v5`, `golang-migrate`, dan `pgregory.net/rapid`) menjadi langkah coding bertahap. Bahasa implementasi adalah Go, sesuai design. Urutan tugas mengikuti fase Product Spec v1.1:

- **Phase 1**: migrasi database, App_Settings, auth/invite/JWT (rotasi Refresh_Token tanpa grace window), content dan translations, CMS admin, progress.
- **Phase 2**: AI foundation (provider abstraction, rate limiting, task service dengan penyimpanan PostgreSQL atau memori, prompt gateway, evaluator dasar, Golden_Set mode `recorded` dan `live`).
- **Phase 3**: assessment engine, free-text evaluation, Evaluation_Retry dan fallback `unscored`, misconception, Mastery v1, rekomendasi rule-based.
- **Phase 4**: interview engine (lifecycle, pause/resume, follow-up allocation, interview evaluation, report, Evaluation_Retry jawaban interview).
- **Opsional (P1)**: RAG/pgvector, hanya di akhir.

Setiap fase berakhir dengan checkpoint. Logika penilaian ditulis sebagai fungsi murni di `internal/evaluation` tanpa I/O (Requirement 26.4). Skor final, status mastery, dan keputusan follow-up selalu dihitung di server, bukan oleh output AI. Parameter scoring, mastery, AI, dan token dibaca dari App_Settings (`app_settings`) sebagai Initial_Value yang dapat diubah Admin, bukan nilai final produk.

Property test dibagi dua kategori (D-16, Requirement 26). Property pada delapan area logika inti **wajib**: sub-tugasnya tidak bertanda `*` dan menjadi gerbang merge CI. Property di luar delapan area itu **opsional** dan tetap bertanda `*`.

## Tasks

### Phase 1: Fondasi, database, App_Settings, auth, konten, CMS, progress

- [ ] 1. Fondasi proyek dan platform
  - [x] 1.1 Inisialisasi module Go, struktur direktori, dan paket `platform`
    - Buat `go.mod`, `cmd/api`, `cmd/worker`, `cmd/migrate`, `internal/platform/{config,db,ids,clock}` sesuai struktur paket pada design.
    - `config`: baca env dan validasi (JWT key, DB URL, CORS origins, secret provider). Secret hanya dari environment.
    - `config` penyimpanan queue dan rate limit (D-15, DA-18): `DEPLOYMENT_MODE` (`multi_instance` default, `single_instance`, `dev`), `RATELIMIT_STORE` dan `QUEUE_STORE` (`postgres` default, `memory`). `memory` hanya valid pada `single_instance` atau `dev`. Kombinasi `multi_instance` dengan `memory` menghentikan proses saat startup dengan pesan konfigurasi.
    - `ids`: UUID v7 (`uuid.NewV7()`). `clock`: interface `Clock` dengan implementasi nyata dan palsu.
    - `db`: `pgxpool`, `SET TIME ZONE 'UTC'`, helper `WithTx` yang mengulang hingga 3 kali pada `40P01` dan `40001` dengan jitter 10 sampai 50 ms.
    - _Requirements: 2.1, 2.2, 16.17, 16.18, 18.6_

  - [~] 1.2 Implementasi `platform/httpx`: Error_Envelope, middleware, dan health check
    - `apperr.Error{Code, HTTPStatus, Details}` dan daftar `code` minimal (`validation_error`, `unauthenticated`, `forbidden`, `not_found`, `translation_not_available`, `conflict`, `rate_limited`, `ai_quota_exceeded`, `ai_queue_limit_exceeded`, `ai_provider_unavailable`, `evaluation_not_retryable`, `internal_error`).
    - Middleware: `request_id` (header `X-Request-ID` dipakai bila cocok `^[A-Za-z0-9_-]{8,64}$`, selain itu UUID baru), log terstruktur (`request_id`, `user_id`, method, path, status, durasi), CORS berbasis daftar origin, `recover` (tanpa stack trace ke client).
    - Prefix `/api/v1`, petakan `validation_error` dengan `details[{field, reason}]`, dan `GET /api/v1/health` tanpa autentikasi yang tidak membocorkan konfigurasi.
    - Pastikan log dan response tidak memuat `password_hash`, `token_hash`, `code_hash`.
    - Batas ukuran body 256 KiB (HTTP 413 `payload_too_large`) sesuai design 2.1.
    - _Requirements: 9.1, 9.2, 9.3, 9.4, 9.8, 10.11, 16.11, 18.1, 18.4, 18.5, 18.7, 25.6_

  - [ ]* 1.3 Write property test untuk Request_ID
    - **Property 6: Request_ID dipakai dari header hanya bila valid**
    - **Kategori:** Opsional (di luar delapan area inti). Nama test `TestPropO6_<Judul>`.
    - Paket `platform/httpx`, fungsi `ResolveRequestID`, oracle regex referensi.
    - **Validates: Requirements 9.4**

  - [~] 1.4 Implementasi helper pagination cursor dan offset
    - Cursor keyset (`limit`, `cursor`, `next_cursor`) dengan decoder yang menolak cursor rusak sebagai `validation_error`.
    - Offset pagination (`limit`, `offset`, `total`). Default dan maksimum `limit` per jenis daftar sesuai tabel pada design 2.3.
    - _Requirements: 9.5, 9.6, 9.7_

  - [ ]* 1.5 Write property test untuk cursor pagination
    - **Property 7: Traversal cursor pagination mengembalikan setiap item tepat sekali**
    - **Kategori:** Opsional (di luar delapan area inti). Nama test `TestPropO7_<Judul>`.
    - Paket `platform/db`, `PageCursor` pada penyimpanan in-memory.
    - **Validates: Requirements 9.5, 9.7**

  - [~] 1.6 Implementasi service App_Settings di `platform/settings`
    - Antarmuka `Settings{Snapshot, Update}` dengan penyimpanan PostgreSQL (`app_settings`) dan in-memory. `Snapshot(keys...)` membaca nilai pada satu titik waktu (cache 30 detik) dan melaporkan `all_initial`.
    - `Update(actor, key, value)`: validasi terhadap `value_schema` (tipe dan rentang) dan aturan lintas kunci (`scoring.clamp_min < scoring.clamp_max`, `interview.min_topics_per_session <= interview.max_topics_per_session`, `scoring.critical_score_cap` dalam rentang clamp, kuota `null` atau bilangan bulat `>= 1`). Nilai valid disimpan dengan `updated_by` terisi dan `is_initial_value = false`. Nilai tidak valid menghasilkan `validation_error` tanpa mengubah baris. Kunci yang belum ada menghasilkan `not_found` (kunci baru tidak dapat dibuat lewat API, DA-19).
    - Snapshot parameter `P` dipakai fungsi murni sebagai argumen, dan nilai yang dipakai dicatat pada hasil (`params_snapshot`, `evidence.parameters`).
    - _Requirements: 24.1, 24.3, 24.5, 24.6, 24.7_

  - [ ]* 1.7 Write property test untuk App_Settings
    - **Property 30: Perubahan App_Settings valid menandai non-initial, nilai tidak valid ditolak**
    - **Kategori:** Opsional (di luar delapan area inti). Nama test `TestPropO30_<Judul>`.
    - Paket `platform/settings`, `Update` dan `Snapshot` pada penyimpanan in-memory dengan model referensi per kunci.
    - **Validates: Requirements 24.3, 24.4, 24.6**

- [ ] 2. Migrasi database (seluruh migrasi P0)
  - [~] 2.1 Siapkan `cmd/migrate` dan tulis migrasi `0001_helpers` serta `0002_locales_auth`
    - `cmd/migrate up|down` dengan `golang-migrate`, opsi `--include-p1` (dipakai pada tugas opsional 26).
    - `0001`: `set_updated_at`, `attach_updated_at_trigger`, `enforce_reviewer_admin`, `attach_translation_triggers`.
    - `0002`: `supported_locales`, `users` (unique `lower(email)`), `invitations`, `refresh_tokens`.
    - Satu file `.up.sql` dan `.down.sql` per migrasi. Enum memakai `text` + `CHECK`.
    - _Requirements: 2.1, 2.2, 2.3, 2.4, 2.5, 3.6, 7.1, 7.2, 7.3_

  - [~] 2.2 Tulis migrasi `0003_content`
    - `topics`, `materials`, `concepts`, `misconceptions` (trigger immutabilitas `code` dan `topic_id`), `questions`, `question_concepts`, `topic_prerequisites`, `roadmaps`, `roadmap_topics`, `user_roadmaps`, serta seluruh tabel `*_translations`.
    - Unique `(entitas, locale)`, partial unique index `published`, constraint `published` mensyaratkan reviewer, `reviewed_at`, dan `published_at`.
    - Soft delete (`deleted_at`) dengan partial index `WHERE deleted_at IS NULL`.
    - _Requirements: 2.8, 3.1, 3.2, 3.3, 3.4, 3.5, 3.7, 3.8, 6.1, 6.2, 6.3, 6.4, 6.5, 20.1, 20.2, 22.6_

  - [~] 2.3 Tulis migrasi `0004_ai_infra`
    - `ai_task_types`, `prompt_contracts` (unique `(task_type, version)`, partial unique `active` per `task_type`, trigger immutabilitas setelah keluar dari `draft`), `ai_provider_settings`, `ai_tasks` (termasuk `idempotency_key` unik), `ai_usage_logs`, `ai_injection_events`, `rate_buckets`.
    - `app_settings` diperluas: kolom `key` unik (`CHECK` pola `^[a-z][a-z0-9_.]*$`), `value` (jsonb), `value_schema` (jsonb objek JSON Schema), `description`, `is_initial_value` (default true), `updated_by` nullable, `created_at`, `updated_at`. Trigger `app_settings_mark_changed` menurunkan `is_initial_value` menjadi false pada setiap perubahan `value`, juga untuk UPDATE langsung ke database.
    - Tidak ada tabel `ai_quota_settings`. Kuota AI disimpan sebagai kunci `ai.quota.{scope}.{period}.{metric}` pada `app_settings`.
    - _Requirements: 15.3, 15.4, 15.11, 16.1, 16.2, 16.5, 16.17, 23.5, 24.1, 24.3_

  - [~] 2.4 Tulis migrasi `0005_assessment` dan `0006_interview`
    - `assessment_attempts` dengan `status` `in_progress`, `submitted`, `scored`, `unscored` (constraint `scored` memerlukan `score_pct` dan `scored_at`, `unscored` tanpa keduanya, `resolved_at` terisi jika dan hanya jika `scored` atau `unscored`).
    - `assessment_answers` dengan `evaluation_status` (`pending`, `scored`, `failed`, `unscored`, null selama draf), `evaluation_retry_count`, `evaluation_failed_at` (kegagalan pertama, tidak di-reset retry, DA-17), `evaluation_prompt_contract_id`, constraint `failed` memerlukan `evaluation_failed_at`, trigger `answer_text_frozen_after_submit`, dan partial index `failed` serta `pending`/`failed`.
    - `interview_profiles`, `interview_sessions` (kolom lengkap termasuk `prompt_contract_versions`, `retention_until`, `deleted_at`), `interview_messages` (unique `(interview_session_id, sequence)`) dengan kolom evaluasi yang sama pada pesan `kind = answer` (`evaluation_status`, `evaluation_retry_count`, `evaluation_failed_at`, `evaluation_prompt_contract_id`) dan constraint `kind = 'answer'` jika dan hanya jika `evaluation_status` terisi.
    - _Requirements: 11.1, 11.2, 11.5, 20.1, 20.11, 20.12, 21.1, 25.1, 25.4_

  - [~] 2.5 Tulis migrasi `0007_evaluation_progress` dan `0008_golden_set`
    - `answer_evaluations`, `answer_evaluation_misconceptions`, `interview_evaluations`, `topic_attempts`, `mastery_records` (unique `(user_id, topic_id)`, `interview_ready` nullable), `user_misconceptions` (unique `(user_id, misconception_id)`).
    - Unique index sumber untuk idempotensi penulisan hasil (`answer_evaluations_aa_uk`, `answer_evaluations_im_uk`, `topic_attempts_*_uk`).
    - `golden_cases`, `golden_runs` (kolom `run_mode` `recorded` atau `live`, `trigger_source`, `baseline_run_id` yang hanya merujuk run `live` berstatus `passed`, `status` termasuk `insufficient_coverage` dan `errored`, `error_reasons`), `golden_run_results`, dan `golden_recordings` (unique `(prompt_contract_id, model_name, golden_case_id)`, `responses` array tidak kosong, `case_sha256`, `source_run_id`).
    - _Requirements: 6.7, 11.15, 12.1, 12.2, 12.3, 12.5, 17.1, 17.3, 17.14, 17.15_

  - [~] 2.6 Tulis migrasi `0009_seed_reference` dan skrip verifikasi migrasi CI
    - Seed: `supported_locales` (`id` default, `en`), `ai_task_types`, `interview_profiles` (Practice 5 sampai 10, 2 follow-up, total cap 10, depth 2. Recruitment 6 sampai 10, 3 follow-up, total cap 15, depth 3, bobot 30/25/20/15/10), `ai_provider_settings` contoh nonaktif.
    - Seed `app_settings` dengan `is_initial_value = true`, `value_schema`, dan `description` untuk seluruh parameter Requirement 24.2: penalti Misconception `minor` dan batasnya, penalti dan batas skor Critical_Misconception, nilai kredit `partial`, batas clamp, ambang metrik Golden_Set (`golden.threshold.*`, `golden.regression_tolerance`), `golden.min_cases_per_category`, `golden.live_schedule_enabled` dan `golden.live_schedule_cron`, kuota AI `ai.quota.{scope}.{period}.{metric}` (delapan kombinasi: tiga ber-Initial_Value, lima `null` tanpa batas, DA-16), `ai.per_user_queue_limit`, TTL Access_Token dan Refresh_Token, `ai.max_validation_retries`, `ai.max_evaluation_retries`, `ai.evaluation_retry_window_hours`, `interview.min_topics_per_session`, `interview.max_topics_per_session`, serta parameter Mastery v1.
    - Seed memakai `ON CONFLICT (key) DO NOTHING` agar seed ulang tidak menimpa nilai yang sudah diubah Admin dan tidak mengembalikan `is_initial_value` ke true. Tidak ada seed `ai_quota_settings`.
    - Skrip CI: `up` dari kosong pada PostgreSQL tanpa pgvector, `down` penuh lalu `up` lagi, bandingkan `pg_dump --schema-only` dengan snapshot yang di-commit.
    - Pastikan migrasi `0001` sampai `0009` tidak bergantung pada `pgvector` maupun Redis.
    - _Requirements: 2.6, 2.7, 19.2, 21.1, 24.1, 24.2_

  - [ ]* 2.7 Write integration test untuk constraint database
    - Dengan `testcontainers-go`: nilai enum tidak valid, translation duplikat, reviewer bukan admin, `published` tanpa reviewer, perubahan `code` Misconception, perubahan isi Prompt_Contract aktif, dua Prompt_Contract `active` untuk satu `task_type`.
    - App_Settings: seed menghasilkan `is_initial_value = true` untuk seluruh kunci Requirement 24.2, seed ulang tidak menimpa nilai yang diubah, trigger `app_settings_mark_changed` menurunkan `is_initial_value` pada UPDATE langsung, tabel `ai_quota_settings` tidak ada.
    - Evaluasi dan Golden_Set: `evaluation_status` di luar nilai valid ditolak, `answer_text` tidak dapat diubah setelah submit, status `failed` tanpa `evaluation_failed_at` ditolak, `run_mode` di luar `recorded`/`live` ditolak, `golden_recordings` duplikat ditolak.
    - _Requirements: 2.5, 3.3, 3.8, 4.4, 6.5, 15.3, 15.11, 17.3, 24.1, 24.2, 24.3, 25.1, 25.4_

- [ ] 3. Auth, invite, dan JWT
  - [~] 3.1 Implementasi primitif kriptografi dan kebijakan password di `internal/auth`
    - `argon2id` (`m=65536`, `t=3`, `p=2`, salt 16 byte, key 32 byte) sebagai PHC string dengan `rehash on login`.
    - Generator Invite_Code dan Refresh_Token (32 byte `crypto/rand`, base64url) dan hash SHA-256.
    - Kebijakan password (panjang minimum dan aturan validasi) dari konfigurasi.
    - _Requirements: 7.4, 7.5, 18.2_

  - [~] 3.2 Implementasi penerbitan dan validasi Access_Token serta middleware role
    - JWT `HS256` dengan klaim `sub`, `role`, `iat`, `exp`, `jti`. Masa berlaku dibaca dari App_Settings lewat `Settings.Snapshot` (`auth.access_token_ttl_seconds` Initial_Value 900, `auth.refresh_token_ttl_seconds` Initial_Value 2.592.000). Perubahan TTL hanya berlaku untuk token yang diterbitkan setelah perubahan.
    - Middleware autentikasi (`unauthenticated` 401) dan `RequireRole` (`forbidden` 403). Endpoint Admin oleh role `user` mengembalikan 403.
    - _Requirements: 8.1, 8.2, 8.8, 8.9, 10.10_

  - [~] 3.3 Implementasi invitation dan register atomik
    - Predikat `InviteUsable` (belum terpakai, belum dicabut, belum kedaluwarsa, email cocok tanpa membedakan huruf besar-kecil).
    - `POST /auth/register` dalam satu transaksi: `SELECT ... FOR UPDATE` undangan, insert user, tandai `used_at` dan `used_by`. Kegagalan memberi `invalid_invite_code` (403) atau `email_already_registered` (409).
    - `POST`, `GET`, `DELETE /admin/invitations`: Invite_Code asli hanya dikembalikan satu kali saat pembuatan, pencabutan hanya untuk undangan belum terpakai.
    - _Requirements: 7.6, 7.7, 7.8, 7.9, 7.10, 10.1, 10.5_

  - [ ]* 3.4 Write property test untuk kegunaan Invite_Code
    - **Property 4: Kegunaan Invite_Code ditentukan oleh empat syarat**
    - **Kategori:** Opsional (di luar delapan area inti). Nama test `TestPropO4_<Judul>`.
    - Paket `auth`, fungsi `InviteUsable`, predikat referensi.
    - **Validates: Requirements 7.8, 7.9**

  - [~] 3.5 Implementasi login, rotasi refresh tanpa grace window, logout, dan endpoint `/me`
    - `POST /auth/login` dengan pesan `invalid_credentials` yang sama untuk email tidak terdaftar dan password salah, isi `last_login_at`.
    - `RotateRefresh` dan `RevokeFamily` sebagai fungsi murni di `internal/evaluation/tokenfamily.go` (waktu dan hash token baru sebagai argumen, Requirement 26.4). Tidak ada grace window: token yang sudah `revoked_at` terisi (termasuk karena rotasi sah) selalu dianggap reuse, berapa pun selisih waktunya. Reuse me-revoke seluruh Token_Family dan menghasilkan `refresh_token_reuse_detected`. Token kedaluwarsa atau tidak dikenal menghasilkan `invalid_refresh_token`. Setiap family memiliki paling banyak satu token aktif.
    - `POST /auth/refresh` membungkus fungsi tersebut dalam satu transaksi dengan `SELECT ... FOR UPDATE` pada baris token (token lama `revoked_at` dan `replaced_by`, token baru dalam family yang sama, TTL dari App_Settings).
    - `POST /auth/logout` me-revoke family. `GET` dan `PATCH /me`, `POST /me/password` (revoke semua Refresh_Token).
    - _Requirements: 7.11, 8.3, 8.4, 8.5, 8.6, 8.7, 8.11, 10.1, 18.3, 26.4_

  - [~] 3.6 Write property test untuk rotasi Refresh_Token
    - **Property 5: Rotasi Refresh_Token menjaga satu token aktif per family dan mendeteksi reuse**
    - **Kategori:** Wajib (area 8: invariant autentikasi token). Nama test `TestPropW5_<Judul>`.
    - Paket `evaluation`, `RotateRefresh` dan `RevokeFamily` pada model Token_Family in-memory dengan operasi acak (refresh dengan token aktif, token yang sudah dirotasi, token tidak dikenal, logout, user dinonaktifkan, password diganti, dan peristiwa waktu). Pemakaian ulang pada selisih waktu nol atau lebih selalu reuse.
    - **Validates: Requirements 8.3, 8.4, 8.5, 8.6, 8.7, 8.11, 26.2**

  - [~] 3.7 Implementasi admin users dan perintah `bootstrap-admin`
    - `GET`, `PATCH /admin/users` (ubah `is_active` dan `role`). Menonaktifkan user me-revoke seluruh Refresh_Token milik user lewat `RevokeFamily`.
    - `cmd/migrate bootstrap-admin --email=...` membaca `BOOTSTRAP_ADMIN_PASSWORD` dari environment dan hanya membuat admin bila belum ada.
    - _Requirements: 8.7, 10.5_

  - [ ]* 3.8 Write property test untuk kebocoran hash dan secret pada DTO
    - **Property 26: Hash dan secret tidak pernah muncul pada response**
    - **Kategori:** Opsional (di luar delapan area inti). Nama test `TestPropO26_<Judul>`.
    - Paket `httpx/dto`, refleksi atas seluruh DTO publik yang sudah ada.
    - **Validates: Requirements 18.1, 7.6**

  - [ ]* 3.9 Write integration test untuk auth
    - 20 goroutine register dengan Invite_Code yang sama: tepat satu berhasil dan tidak ada user yatim.
    - Dua `refresh` bersamaan dengan token yang sama: satu `issued`, satu `refresh_token_reuse_detected`, seluruh family ter-revoke, dan `refresh` ketiga dengan token baru juga ditolak.
    - Log request tidak memuat hash, password, atau token. Endpoint Admin oleh `user` mengembalikan `forbidden`.
    - _Requirements: 7.7, 7.10, 8.4, 8.11, 10.10, 18.1, 18.4_

  - [~] 3.10 Tulis contoh klien single-flight refresh untuk Astro dan Svelte
    - Direktori `examples/web-client`: modul `refreshSingleFlight.ts` yang menjamin paling banyak satu request `refresh` berjalan per Refresh_Token pada seluruh permintaan paralel dan seluruh tab. Dalam tab: satu promise bersama. Antar tab: `navigator.locks` dengan `BroadcastChannel` untuk menyebarkan pasangan token baru, dan mutex `localStorage` sebagai cadangan. Setelah memegang kunci, klien membaca ulang token tersimpan dan memakai pasangan yang sudah dirotasi tab lain tanpa mengirim `refresh` kedua.
    - `apiClient.ts`: fetch wrapper yang pada 401 `unauthenticated` menunggu hasil single-flight lalu mengulang request satu kali. `refresh_token_reuse_detected` dan `invalid_refresh_token` membersihkan token dan mengarahkan ke login.
    - Contoh integrasi: Svelte store `auth` dan middleware Astro. Lokasi penyimpanan token dibuat sebagai antarmuka `TokenStorage` karena belum diputuskan (DA-20) dan tidak memengaruhi kontrak server.
    - _Requirements: 8.11, 8.12_

  - [ ]* 3.11 Write test untuk contoh klien single-flight
    - Vitest dengan dua tab tiruan dan sepuluh permintaan paralel pada Access_Token kedaluwarsa: server palsu hanya menerima satu `refresh`, seluruh permintaan memakai pasangan token yang sama, dan tidak ada `refresh_token_reuse_detected`.
    - _Requirements: 8.12_

- [ ] 4. Konten bilingual dan workflow Translation
  - [~] 4.1 Implementasi resolusi Locale dan filter Published_Translation sebagai fungsi murni
    - `internal/evaluation/locale.go`: `ParseAcceptLanguage`, `ResolveLocale` (urutan query, `Accept-Language`, preferensi user, Default_Locale, pemetaan `id-ID` ke `id`), `FilterPublished`, `GetDetail` (`translation_not_available` dengan `available_locales` terurut).
    - _Requirements: 5.1, 5.2, 5.3, 5.9_

  - [ ]* 4.2 Write property test untuk resolusi Locale
    - **Property 1: Resolusi Locale selalu menghasilkan Locale yang didukung**
    - **Kategori:** Opsional (di luar delapan area inti). Nama test `TestPropO1_<Judul>`.
    - **Validates: Requirements 5.1, 5.2, 5.3**

  - [~] 4.3 Implementasi workflow Translation sebagai fungsi murni dan service
    - `ApplyTransition` dan `ApplyEdit` di paket `content`: tabel transisi valid (`draft`→`in_review`, `in_review`→`draft`, `in_review`→`published`, `published`→`archived`, `archived`→`draft`), `invalid_status_transition` dan `version_conflict` (409).
    - Translation baru berstatus `draft` dengan `version` 1. Ubah teks menaikkan `version` lewat `UPDATE ... WHERE version = $2`. Edit pada `published` mengembalikan ke `draft` dan mengosongkan `published_at`. Transisi ke `published` mensyaratkan `reviewer_id` ber-role `admin` dan mengisi `reviewed_at`, `published_at`.
    - _Requirements: 4.1, 4.2, 4.3, 4.4, 4.5, 4.6, 4.7, 4.8, 26.4_

  - [~] 4.4 Write property test untuk workflow Translation
    - **Property 3: Workflow Translation mengikuti state machine dan versioning**
    - **Kategori:** Wajib (area 7: aturan status dan lifecycle, Translation). Nama test `TestPropW3_<Judul>`.
    - **Validates: Requirements 4.2, 4.3, 4.4, 4.5, 4.6, 4.7, 26.2**

  - [~] 4.5 Implementasi endpoint konten Learner
    - `GET /topics`, `/topics/{id}` (id atau slug), `/topics/{id}/materials`, `/materials/{id}`, `/roadmaps`, `/roadmaps/{id}` dengan parameter `locale`.
    - Hanya entitas ber-Published_Translation pada Locale permintaan. Setiap response memuat field `locale` dan header `Content-Language`. Daftar memakai cursor pagination.
    - _Requirements: 5.4, 5.5, 5.6, 5.7, 5.8, 10.2, 20.3_

  - [ ]* 4.6 Write property test untuk penyajian konten
    - **Property 2: Konten hanya disajikan dari Published_Translation pada Locale permintaan**
    - **Kategori:** Opsional (di luar delapan area inti). Nama test `TestPropO2_<Judul>`.
    - **Validates: Requirements 5.4, 5.5, 5.6, 5.7, 5.8, 20.3**

- [ ] 5. CMS admin
  - [~] 5.1 Implementasi CRUD admin untuk entitas induk
    - `GET`, `POST /admin/{resource}` dan `GET`, `PATCH`, `DELETE /admin/{resource}/{id}` untuk `topics`, `materials`, `concepts`, `questions`, `roadmaps`, `misconceptions`.
    - DELETE berupa soft delete atau `is_active=false`. Daftar admin memakai offset pagination. Hanya role `admin`.
    - Kelola `question_concepts` (`weight`, `is_required`) dan `roadmap_topics`.
    - _Requirements: 3.1, 3.7, 10.6, 20.2_

  - [~] 5.2 Implementasi endpoint Translation admin
    - `PUT /admin/{resource}/{id}/translations/{locale}` dan `POST .../transitions` yang memakai service pada 4.3.
    - Tolak transisi `published` untuk Question tanpa Concept atau dengan `reference_answer` kosong.
    - _Requirements: 3.2, 3.4, 4.3, 4.4, 10.6, 20.4_

  - [~] 5.3 Implementasi katalog Misconception dan prasyarat Topic
    - Buat, ubah `severity` dan Translation, serta nonaktifkan Misconception (`is_active=false` tanpa menghapus baris dan tanpa mengubah `user_misconceptions`). `code` tidak dapat diubah.
    - `PUT /admin/topics/{id}/prerequisites` dengan pencegahan siklus pada DAG.
    - _Requirements: 6.2, 6.3, 6.4, 6.5, 6.6, 10.7, 22.6_

  - [ ]* 5.4 Write unit test untuk CMS admin
    - RBAC (user biasa mendapat `forbidden`), aturan publish Question, soft delete tidak muncul pada response Learner, siklus prasyarat ditolak, nonaktifkan Misconception tidak mengubah `user_misconceptions`.
    - _Requirements: 6.6, 10.6, 10.10, 20.4_

- [ ] 6. Progress Learner
  - [~] 6.1 Implementasi repository dan endpoint progress
    - `GET /me/progress`, `GET /me/progress/topics/{topic_id}` (`NOT_STARTED` bila belum ada), `GET /me/misconceptions?status=detected|resolved`.
    - Response memuat `interview_ready` nullable dan `evidence`. Learner hanya membaca data milik sendiri.
    - _Requirements: 10.4, 12.2, 12.5, 12.7, 18.3_

  - [ ]* 6.2 Write unit test untuk endpoint progress
    - Isolasi antar user (`not_found`), default `NOT_STARTED`, `interview_ready` bernilai null.
    - _Requirements: 12.5, 12.7, 18.3_

- [~] 7. Checkpoint Phase 1
  - Ensure all tests pass, ask the user if questions arise.
  - Jalankan juga property wajib Phase 1 (Property 3 dan 5): `go test -race -rapid.checks=100 -run '^TestPropW' ./...`.

### Phase 2: AI foundation

- [ ] 8. Provider abstraction
  - [~] 8.1 Implementasi interface `Provider`, tipe error, dan adapter
    - `internal/ai/provider`: `Provider{Name, Complete}`, error bertipe `RateLimited{RetryAfter}`, `Timeout`, `Server`, `Client`.
    - Adapter provider sesuai `ai_provider_settings` (RPM, TPM, urutan fallback) dan `provider/replay` yang memutar respons terekam (dari `golden_recordings` atau fixture) untuk mode `recorded` dan CI.
    - _Requirements: 16.9, 16.15, 17.12_

  - [~] 8.2 Implementasi `BackoffDelay` dan `SelectProvider`
    - Exponential backoff dengan jitter (nilai acak disuntikkan), menghormati `Retry-After`. `SelectProvider` memilih provider berikutnya menurut urutan fallback dan cooldown. Keduanya fungsi murni (Requirement 26.4).
    - _Requirements: 16.8, 16.9, 26.4_

  - [~] 8.3 Write property test untuk backoff dan pemilihan provider
    - **Property 21: Backoff berbatas dan pemilihan provider monoton**
    - **Kategori:** Wajib (area 6: token bucket dan backoff). Nama test `TestPropW21_<Judul>`.
    - **Validates: Requirements 16.8, 16.9, 26.2**

- [ ] 9. Rate limiting
  - [~] 9.1 Implementasi token bucket dan middleware HTTP
    - `platform/ratelimit`: `TryTake` sebagai fungsi murni (Requirement 26.4) dan interface `BucketStore.TryTake` dengan implementasi `memory` dan `postgres` (`rate_buckets`, `SELECT ... FOR UPDATE`) dipilih lewat `RATELIMIT_STORE` (default `postgres`, `memory` hanya single-instance atau dev). Bucket RPM dan TPM per provider memakai store yang sama sehingga dibagi antar instance pada mode `postgres`.
    - Middleware per kelas endpoint (auth, AI, umum) dengan `capacity` dan `refill_rate`. Respons 429 `rate_limited` dengan header `Retry-After` dalam detik.
    - Fungsi `PurgeIdleBuckets` untuk menghapus bucket penuh yang tidak aktif lebih dari 1 jam (dijadwalkan pada tugas 10.6).
    - _Requirements: 9.9, 16.6, 16.7, 16.15, 16.17, 16.18, 26.4_

  - [~] 9.2 Pasang rate limit pada endpoint auth
    - Batasi `login` dan `register` per alamat IP dan per email memakai bucket pada 9.1.
    - _Requirements: 8.10, 9.9_

  - [~] 9.3 Write property test untuk token bucket
    - **Property 20: Token bucket menjaga batas kapasitas dan laju**
    - **Kategori:** Wajib (area 6: token bucket dan backoff). Nama test `TestPropW20_<Judul>`.
    - **Validates: Requirements 16.6, 16.7, 26.2**

  - [ ]* 9.4 Write integration test untuk konfigurasi penyimpanan
    - `DEPLOYMENT_MODE=multi_instance` dengan `RATELIMIT_STORE=memory` atau `QUEUE_STORE=memory` ditolak saat startup. Dua instance `api` berbagi bucket `rate_buckets` pada mode `postgres`.
    - _Requirements: 16.17, 16.18_

- [ ] 10. Task service
  - [~] 10.1 Implementasi `NextTaskState` sebagai fungsi murni
    - `internal/evaluation/tasklife.go`: transisi hanya `queued`→`processing` (`run_after <= now`), `processing`→`completed`, `processing`→`queued` (`requeue` dengan `count_attempt`), `processing`→`failed`, dan `lock_expired` (kembali ke `queued` bila `attempts < max_attempts`, selain itu `failed` dengan `ai_provider_unavailable`). `completed` dan `failed` terminal, transisi lain menghasilkan `invalid_task_transition`.
    - Invarian: `status = processing` jika dan hanya jika `locked_at` terisi, `completed` memiliki `result`, `failed` memiliki `error_code`, `0 <= attempts <= max_attempts`.
    - _Requirements: 16.5, 16.10, 16.13, 26.4_

  - [~] 10.2 Write property test untuk siklus status AITask
    - **Property 29: Siklus status AITask mengikuti state machine dan menjaga invarian**
    - **Kategori:** Wajib (area 7: aturan status dan lifecycle, AITask). Nama test `TestPropW29_<Judul>`.
    - Urutan peristiwa acak (`dequeue`, `complete`, `requeue`, `fail`, `lock_expired`) pada waktu non-menurun terhadap `NextTaskState`.
    - **Validates: Requirements 16.5, 16.10, 16.13, 26.2**

  - [~] 10.3 Implementasi `Queue` AITask pada PostgreSQL dan versi in-memory
    - `internal/ai/taskqueue`: `Enqueue`, `Dequeue` (`FOR UPDATE SKIP LOCKED`, urut `(priority, run_after, created_at)`), `Complete`, `Requeue`, `Fail`, `ReapStale`. Setiap perubahan status memakai `NextTaskState`.
    - `Enqueue` menghormati `idempotency_key` (unique `ai_tasks_idem_uk`): kunci sama dari user sama mengembalikan task yang sama (header `Idempotency-Key`, design 2.1). Batas `per_user_queue_limit` diperiksa atomik dan semua atau tidak sama sekali (`ai_queue_limit_exceeded`).
    - `ReapStale` juga menaikkan task priority 3 yang menunggu lebih dari 15 menit menjadi priority 2 (aging, design 4.9).
    - Pilihan penyimpanan lewat `QUEUE_STORE`: `postgres` (default, wajib untuk multi-instance) atau `memory` (penjadwal in-process dengan write-through ke `ai_tasks`, hanya single-instance atau dev, worker satu proses dengan API lewat `api --with-worker`). Kontrak API dan endpoint status tidak berubah antar mode.
    - Satu suite test kontrak yang dijalankan pada kedua implementasi.
    - _Requirements: 16.5, 16.11, 16.12, 16.13, 16.15, 16.17, 16.18_

  - [~] 10.4 Write property test untuk antrean AITask
    - **Property 22: Antrean AITask memilih menurut prioritas dan menolak melebihi batas per user**
    - **Kategori:** Wajib (area 7: aturan status dan lifecycle, AITask). Nama test `TestPropW22_<Judul>`.
    - Implementasi in-memory dari antarmuka `Queue`, pengurutan referensi.
    - **Validates: Requirements 16.11, 16.12, 26.2**

  - [~] 10.5 Implementasi Quota_Manager
    - Baca kuota dari App_Settings (kunci `ai.quota.{scope}.{period}.{metric}`, scope `global` dan `user`, periode harian dan bulanan, metrik `requests` atau `total_tokens`, nilai `null` berarti tanpa batas, seluruh delapan kombinasi disemai sehingga dapat diubah Admin, DA-16) lewat `Settings.Snapshot`. Pemakaian periode berjalan dihitung dari `ai_usage_logs` sebelum task diterima. Tidak ada tabel `ai_quota_settings` dan tidak ada override kuota per user di P0.
    - Kembalikan 429 `ai_quota_exceeded` dengan `details.reset_at` (UTC, awal periode berikutnya) dan 429 `ai_queue_limit_exceeded` bila melewati `ai.per_user_queue_limit`. Perubahan kuota oleh Admin berlaku pada `Enqueue` berikutnya tanpa deploy.
    - _Requirements: 16.2, 16.3, 16.4, 16.11, 24.2_

  - [~] 10.6 Implementasi worker runner dan endpoint status AITask
    - `cmd/worker`: loop dequeue, registry handler per `task_type`, reaper `lock_timeout`, penjadwal job (termasuk `PurgeIdleBuckets`), graceful shutdown (timeout 30 detik).
    - `GET /ai-tasks/{id}` (pemilik dan admin, tanpa `payload`, `result`, atau teks jawaban) dan `GET /admin/ai-tasks` dengan filter.
    - _Requirements: 10.12, 16.5, 16.13, 16.16, 16.17_

  - [ ]* 10.7 Write integration test untuk antrean
    - 8 worker paralel mengambil 500 task tanpa task ganda. Reaper mengembalikan task basi ke `queued`.
    - _Requirements: 16.13_

- [ ] 11. Prompt gateway
  - [~] 11.1 Implementasi Prompt_Contract, registry, dan `seed-prompts`
    - Berkas `prompts/{answer_evaluation,interview_followup,interview_evaluation}/v1.json` (embed `go:embed`) dengan `contract_id`, `task_type`, `version`, `locale_policy`, `input_schema`, `output_schema`, `system_prompt`, `parameters`, `status`, serta contoh input, output valid, dan output tidak valid.
    - Loader dari tabel `prompt_contracts` dan `cmd/migrate seed-prompts` yang idempoten berdasarkan `(task_type, version, content_sha256)`.
    - AI tutoring tidak termasuk P0 (hanya ekstensi lewat Task_Type baru).
    - _Requirements: 15.1, 15.2, 15.5, 15.8, 15.9_

  - [~] 11.2 Implementasi guard anti prompt injection dan `Render`
    - `ai/guard`: `Sanitize` (netralkan string mirip delimiter, nonce aktif, karakter kontrol, panjang maksimum) dan detektor heuristik yang menulis `ai_injection_events` tanpa menghentikan penilaian.
    - `ai/prompt.Render`: dua pesan. `system` tanpa teks Learner, `user` berisi isi Learner di antara delimiter dengan nonce acak per pemanggilan.
    - _Requirements: 15.7, 23.1, 23.2, 23.5_

  - [ ]* 11.3 Write property test untuk penyusunan prompt
    - **Property 25: Penyusunan prompt memisahkan dan menetralkan isi Learner**
    - **Kategori:** Opsional (di luar delapan area inti). Nama test `TestPropO25_<Judul>`.
    - Tambahkan fuzz test `go test -fuzz` untuk `guard.Sanitize`.
    - **Validates: Requirements 23.1, 23.2**

  - [~] 11.4 Implementasi validator JSON Schema dan `ValidateEvaluatorOutput`
    - `ai/schema` dengan `additionalProperties: false`. Pemeriksaan katalog: `misconception_codes` dalam katalog aktif Topic, `resolved_misconception_codes` dalam daftar `detected` Learner, kedua daftar tidak beririsan, `concept_results` tepat satu entri per Concept input, `score_hint` 0 sampai 100, `confidence` 0 sampai 1.
    - _Requirements: 14.3, 14.4, 14.5, 23.3_

  - [ ]* 11.5 Write property test untuk validator output Evaluator
    - **Property 10: Validator output Evaluator menolak setiap pelanggaran kontrak**
    - **Kategori:** Opsional (di luar delapan area inti). Nama test `TestPropO10_<Judul>`.
    - Mutator pada output valid (field tambahan termasuk `severity`, kode di luar input, Concept hilang atau ganda, irisan kode, nilai di luar rentang).
    - **Validates: Requirements 14.3, 14.4, 14.5, 23.3**

  - [~] 11.6 Implementasi pipeline `Gateway.Run`
    - Render prompt, ambil token bucket RPM dan TPM provider (jadwalkan ulang AITask dengan `run_after` bila kurang), panggil provider, validasi output, ulang hingga `ai.max_validation_retries` (default 2), fallback provider, tetapkan `failed` dengan `ai_provider_unavailable` atau `validation_failed`.
    - Tulis satu baris `ai_usage_logs` per percobaan, termasuk yang gagal. Pemanggilan provider tidak berada di dalam transaksi database.
    - _Requirements: 14.6, 16.7, 16.8, 16.9, 16.10, 16.14, 23.4_

  - [ ]* 11.7 Write integration test untuk gateway dengan provider replay
    - Skenario provider 429 lalu fallback, output tidak valid lalu retry, semua provider gagal, `ai_usage_logs` ditulis per percobaan.
    - _Requirements: 14.6, 16.8, 16.9, 16.10, 16.14_

- [ ] 12. Evaluator dasar
  - [~] 12.1 Implementasi `ScoreAnswer` dan pengambilan parameter
    - `internal/evaluation/scoring.go`: skor rubrik dari `concept_results` berbobot, penalti Misconception `minor` (dibatasi `minor_penalty_cap`), batas `critical_score_cap`, skor `clamp_min` untuk `relevance` `off_topic` atau `empty`, `clamp` ke `[clamp_min, clamp_max]`, `round2` half-up dengan desimal presisi tetap. Fungsi murni (Requirement 26.4).
    - `severity` diambil dari database, `score_hint` hanya sinyal konsistensi (`needs_review`).
    - Parameter `P.x` diambil sebagai snapshot dari `Settings.Snapshot` (tugas 1.6) dan dicatat pada `params_snapshot`. Seluruh nilai default adalah Initial_Value, bukan nilai final.
    - _Requirements: 13.11, 14.8, 14.9, 20.8, 20.9, 24.5, 24.7, 26.4_

  - [~] 12.2 Write property test untuk batas dan determinisme skor
    - **Property 11: Skor jawaban berada dalam batas, deterministik, dan tidak bergantung pada skor AI**
    - **Kategori:** Wajib (area 1: scoring free-text). Nama test `TestPropW11_<Judul>`.
    - **Validates: Requirements 13.11, 14.9, 20.8, 20.9, 23.4, 26.2**

  - [~] 12.3 Write property test untuk monotonisitas skor
    - **Property 12: Skor jawaban monoton terhadap coverage Concept**
    - **Kategori:** Wajib (area 1: scoring free-text). Nama test `TestPropW12_<Judul>`.
    - **Validates: Requirements 20.8, 26.2**

  - [~] 12.4 Implementasi service Evaluator `answer_evaluation`
    - Susun input kontrak (pertanyaan, Concept, jawaban sebagai data tidak tepercaya, Locale, Misconception aktif tanpa `severity`, kode `detected` milik Learner), panggil `Gateway.Run`, lalu `ScoreAnswer`. Belum menyimpan hasil (penyimpanan ada pada tugas 17.1).
    - Instruksi prompt membatasi pilihan `code` hanya dari input dan melarang output `severity` atau skor final.
    - _Requirements: 14.1, 14.2, 14.7, 14.9_

- [ ] 13. Golden set
  - [~] 13.1 Implementasi `ValidateCase` dan endpoint CRUD Golden_Set
    - `internal/golden`: validasi `expected_score_min <= expected_score_max`, kode Misconception dalam katalog Topic, kode Concept dalam Concept pertanyaan, Concept tidak ada pada found dan missed sekaligus, `locale` memiliki Published_Translation pertanyaan.
    - `GET`, `POST`, `PATCH`, `DELETE /admin/golden-cases` (offset pagination, hanya `admin`).
    - _Requirements: 10.8, 17.1, 17.4, 17.11_

  - [ ]* 13.2 Write property test untuk validasi Golden case
    - **Property 23: Validasi Golden case**
    - **Kategori:** Opsional (di luar delapan area inti). Nama test `TestPropO23_<Judul>`.
    - **Validates: Requirements 17.4**

  - [~] 13.3 Implementasi metrik, gate, regresi, baseline, dan Runner inti
    - `ComputeGoldenMetrics` (`score_in_range_rate`, `concept_f1`, `critical_recall`, `false_critical_rate`, `injection_resilience_rate`, `catalog_violation_count`, `schema_violation_count`), `EvaluateGate` (`insufficient_coverage`, ambang lulus dari `golden.threshold.*`, toleransi regresi `golden.regression_tolerance` terhadap baseline), dan `SelectBaseline` (hanya run `live` berstatus `passed` milik versi aktif, tidak pernah run `recorded`). Seluruh ambang adalah Initial_Value pada App_Settings.
    - Runner inti memakai pipeline produksi (`Gateway.Run` lalu `ScoreAnswer`) melalui antarmuka sumber response per kasus, yang diimplementasikan oleh mode `live` dan `recorded` pada tugas 13.5.
    - Kategori: `different_wording`, `partially_correct`, `critical_misconception`, `off_topic_or_empty`, `adversarial_injection`. `golden.min_cases_per_category` default 5.
    - _Requirements: 17.2, 17.5, 17.6, 17.7, 17.8, 17.9, 17.10, 17.15, 24.5_

  - [ ]* 13.4 Write property test untuk metrik dan gate Golden_Set
    - **Property 24: Metrik Golden_Set terbatas dan gate konsisten**
    - **Kategori:** Opsional (di luar delapan area inti). Nama test `TestPropO24_<Judul>`.
    - **Validates: Requirements 17.6, 17.7, 17.8, 17.9**

  - [~] 13.5 Implementasi mode `recorded` dan `live`, perekaman response, dan endpoint run
    - `POST /admin/golden-runs` dengan `run_mode` wajib (`recorded` atau `live`), `GET /admin/golden-runs` dan `/{id}`. Menyimpan `run_mode`, `prompt_contract_id`, `model_name`, `trigger_source`, hasil per kasus, dan metrik agregat.
    - `recorded` (dipakai CI, `trigger_source = ci`): menilai response terekam dari `golden_recordings` untuk pasangan (`prompt_contract_id`, `model_name`) lewat `provider/replay`, tanpa memanggil AI provider dan tanpa AITask. Rekaman hilang atau `case_sha256` basi menghasilkan run `errored` (`recording_missing:<case_id>`, `recording_stale:<case_id>`), bukan `passed`.
    - `live` (manual oleh Admin atau job terjadwal di worker lewat `golden.live_schedule_enabled` dan `golden.live_schedule_cron`, `trigger_source = schedule`, di luar pipeline CI): memanggil model nyata lewat `Gateway.Run` sebagai AITask priority 3 yang tunduk pada kuota dan rate limit. Setelah tiap kasus selesai, upsert `golden_recordings` (A-26, DA-14) berisi urutan seluruh response (termasuk yang gagal validasi) dan `case_sha256`.
    - `baseline_run_id` hanya menerima run `live` berstatus `passed`. Run `recorded` tidak pernah menjadi baseline regresi.
    - _Requirements: 10.8, 17.3, 17.12, 17.13, 17.14, 17.15_

  - [ ]* 13.6 Write property test untuk baseline dan gate aktivasi live
    - **Property 31: Hanya run live yang menjadi baseline dan syarat aktivasi**
    - **Kategori:** Opsional (di luar delapan area inti). Nama test `TestPropO31_<Judul>`.
    - Paket `golden`, `SelectBaseline` dan fungsi gate aktivasi (alasan `run_not_live`). Run `recorded` dengan rekaman hilang atau basi menghasilkan `errored`.
    - **Validates: Requirements 15.10, 17.12, 17.13, 17.14, 17.15**

  - [~] 13.7 Siapkan fixture Golden_Set v0
    - `test/golden`: sedikitnya `min_cases_per_category` kasus per kategori untuk Topic contoh, termasuk lima kasus `adversarial_injection` (perintah abaikan instruksi, tiruan delimiter, tiruan JSON output). Label memakai `code` katalog.
    - Sediakan rekaman fixture yang dijalankan sebagai run `recorded` melalui provider replay pada CI (tanpa AI provider).
    - _Requirements: 17.2, 17.5, 17.12, 23.6_

- [ ] 14. Admin konfigurasi AI dan aktivasi Prompt_Contract
  - [~] 14.1 Implementasi endpoint konfigurasi provider AI
    - `GET`, `PUT /admin/ai-provider-settings` (provider, RPM, TPM, urutan fallback). Perubahan berlaku tanpa deploy ulang. Kuota AI tidak memakai tabel atau endpoint terpisah dan dikelola lewat App_Settings pada tugas 14.4.
    - _Requirements: 10.9, 16.2_

  - [~] 14.2 Implementasi endpoint Prompt_Contract dan gate aktivasi
    - `GET`, `POST /admin/prompt-contracts` (versi baru berstatus `draft`) dan `POST /admin/prompt-contracts/{id}/activate`.
    - Aktivasi `answer_evaluation` mensyaratkan run Golden_Set terbaru untuk versi calon yang `passed`, berasal dari `run_mode = live`, dan tidak regresi terhadap baseline `live` (error `golden_gate_failed`, alasan termasuk `run_not_live` untuk run `recorded`), atau `waiver_reason` tercatat. `interview_followup` dan `interview_evaluation` diaktifkan tanpa gate Golden_Set. Retire versi lama dan aktifkan versi baru dalam satu transaksi. Versi di luar `draft` immutable.
    - _Requirements: 10.9, 15.3, 15.10, 15.11, 17.15_

  - [ ]* 14.3 Write integration test untuk gate aktivasi dan pinning prompt
    - Setiap alasan `golden_gate_failed` (termasuk `run_not_live`) dan jalur `waiver_reason`. Run `recorded` mereproduksi metrik run `live` tanpa memanggil provider (provider palsu gagal bila dipanggil), dan rekaman basi menghasilkan `errored`.
    - _Requirements: 15.10, 15.11, 17.12, 17.14, 17.15_

  - [~] 14.4 Implementasi endpoint App_Settings dan view kuota AI
    - `GET /admin/settings` (offset pagination, filter `prefix`), `GET`, `PUT /admin/settings/{key}`, dan `GET`, `PUT /admin/ai-quota-settings` sebagai view atas kunci `ai.quota.*` tanpa tabel sendiri (validasi, `is_initial_value`, dan `updated_by` sama dengan `/admin/settings`). Hanya role `admin`.
    - Setiap item memuat `key`, `value`, `description`, `is_initial_value`, `updated_by`, `updated_at`. `PUT` memakai `Settings.Update` (tugas 1.6): nilai gagal `value_schema` atau aturan lintas kunci menghasilkan 400 `validation_error`, kunci baru menghasilkan `not_found`, perubahan valid menetapkan `is_initial_value = false`.
    - _Requirements: 10.9, 10.15, 16.2, 24.3, 24.4, 24.6_

  - [ ]* 14.5 Write integration test untuk App_Settings
    - `PUT` mengubah `is_initial_value` menjadi false dan mengisi `updated_by`. Nilai tidak valid ditolak 400. Mengubah `ai.quota.user.daily.requests` berlaku pada `Enqueue` berikutnya tanpa deploy, nilai `null` berarti tanpa batas, `reset_at` benar untuk harian dan bulanan. Akses oleh role `user` mendapat `forbidden`.
    - _Requirements: 10.10, 16.2, 16.3, 16.4, 24.3, 24.4, 24.6_

- [~] 15. Checkpoint Phase 2
  - Ensure all tests pass, ask the user if questions arise.
  - Jalankan juga property wajib hingga Phase 2 (Property 3, 5, 20, 21, 22, 29): `go test -race -rapid.checks=100 -run '^TestPropW' ./...`.

### Phase 3: Assessment, free-text evaluation, Evaluation_Retry, Mastery v1, rekomendasi

- [ ] 16. Assessment engine
  - [~] 16.1 Implementasi pembuatan attempt dan pembacaan hasil
    - `POST /assessments/attempts`: pilih Question `published` pada Topic dan Locale dengan `purpose` `assessment` atau `both`, sertakan sedikitnya satu pertanyaan `application` atau `scenario` bila tersedia. Error `insufficient_questions` dan `translation_not_available`.
    - `GET /assessments/attempts` (cursor), `GET /assessments/attempts/{id}` dengan umpan balik per pertanyaan. Setiap jawaban menampilkan `evaluation_status` dan `evaluation_retry_count`, dan attempt dapat berstatus `unscored`. Kepemilikan dilanggar menghasilkan `not_found`.
    - _Requirements: 10.12, 20.3, 20.5, 20.6, 25.11_

  - [~] 16.2 Implementasi `ApplyEvaluationEvent` sebagai fungsi murni
    - `internal/evaluation/evalstate.go`: satu-satunya tempat aturan perpindahan Answer_Evaluation_Status untuk jawaban assessment dan jawaban interview. Peristiwa `submit`, `eval_ok`, `eval_failed`, `retry_requested`, `window_tick`, `session_closed`. Efek `enqueue_evaluation` dan hasil `noop_200` atau `evaluation_not_retryable` dikembalikan ke pemanggil (fungsi tidak melakukan I/O, Requirement 26.4).
    - Jawaban kosong menjadi `scored` saat `submit` tanpa efek enqueue, jawaban non-kosong menjadi `pending` dengan satu efek `enqueue_evaluation`. `eval_failed` mengisi `failed_at` hanya pada kegagalan pertama dan menghasilkan `unscored` jika `retry_count >= max_evaluation_retries`, selain itu `failed`. `retry_requested` pada `failed` dengan `retry_count < max` menghasilkan `pending`, `retry_count + 1`, dan satu efek enqueue. `retry_requested` pada `pending` atau `scored` idempoten (`noop_200`), pada `unscored`, `null`, atau `failed` dengan retry habis menghasilkan `evaluation_not_retryable`. `window_tick` menjadikan `failed` sebagai `unscored` setelah `evaluation_retry_window_hours`. `scored` dan `unscored` terminal, isi jawaban tidak pernah berubah.
    - _Requirements: 20.7, 25.1, 25.3, 25.4, 25.5, 25.6, 25.8, 25.9, 26.4_

  - [~] 16.3 Implementasi simpan jawaban dan submit
    - `PUT /assessments/attempts/{id}/answers/{question_id}` (`attempt_not_active`).
    - `POST .../submit` (202) dalam satu transaksi: ubah status attempt menjadi `submitted`, terapkan `ApplyEvaluationEvent(submit)` per jawaban, periksa kuota dan batas antrean (Quota_Manager), enqueue satu AITask `answer_evaluation` per jawaban non-kosong (menghormati header `Idempotency-Key`, menyimpan `evaluation_prompt_contract_id` dengan versi aktif saat submit), dan beri skor `clamp_min` (0 pada Initial_Value) tanpa AI untuk jawaban kosong (jawaban kosong langsung `scored` dan ikut skor Attempt, DA-15).
    - _Requirements: 16.3, 16.11, 20.5, 20.7, 25.1_

  - [~] 16.4 Implementasi Evaluation_Retry untuk jawaban assessment
    - `POST /assessments/attempts/{attempt_id}/answers/{question_id}/evaluation/retry`, hanya pemilik jawaban (user lain, termasuk admin, mendapat `not_found`). Satu transaksi: `SELECT ... FOR UPDATE` jawaban, `ApplyEvaluationEvent(retry_requested)`, lalu enqueue satu AITask `answer_evaluation` dengan Prompt_Contract yang sama dengan evaluasi sebelumnya (`evaluation_prompt_contract_id`) dan `idempotency_key` `eval-retry:{answer_id}:{retry_count}`.
    - Hasil: 202 bila AITask baru dibuat, 200 dengan status saat ini bila sudah `pending` atau `scored` (tanpa AITask baru dan tanpa menaikkan `evaluation_retry_count`), 409 `evaluation_not_retryable` (`details.reason`) untuk `unscored`, belum disubmit, atau retry habis. Rate limit kelas AI, kuota, dan `per_user_queue_limit` berlaku seperti AITask lain. Bila `rate_limited`, `ai_quota_exceeded`, atau `ai_queue_limit_exceeded`, transaksi di-rollback dan status serta `evaluation_retry_count` tidak berubah. Isi jawaban tidak berubah. Retry paralel menghasilkan tepat satu AITask.
    - _Requirements: 10.14, 16.3, 16.4, 16.11, 25.2, 25.3, 25.4, 25.5, 25.6, 25.7_

  - [~] 16.5 Write property test untuk siklus Answer_Evaluation_Status
    - **Property 27: Siklus Answer_Evaluation_Status dan Evaluation_Retry idempoten**
    - **Kategori:** Wajib (area 7: aturan status dan lifecycle, Answer_Evaluation_Status). Nama test `TestPropW27_<Judul>`.
    - Paket `evaluation`, `ApplyEvaluationEvent` dan model `RetryEvaluation` dengan `Enqueue` palsu (termasuk Enqueue yang gagal oleh rate limit, kuota, atau antrean), urutan peristiwa acak pada waktu non-menurun.
    - **Validates: Requirements 25.1, 25.3, 25.4, 25.5, 25.6, 25.7, 25.8, 25.9, 26.2**

  - [ ]* 16.6 Write unit test untuk assessment
    - Pemilihan pertanyaan (pertanyaan aplikatif disertakan), jawaban kosong tanpa AITask, submit ditolak saat kuota habis dan jawaban tetap tersimpan.
    - _Requirements: 20.6, 20.7, 16.4_

  - [ ]* 16.7 Write integration test Evaluation_Retry assessment end-to-end
    - Provider palsu gagal sehingga jawaban `failed`. Retry pertama 202 dan membuat satu AITask dengan Prompt_Contract yang sama. 20 goroutine retry bersamaan menghasilkan satu AITask dan `evaluation_retry_count` 1. Gagal lagi menjadi `unscored` dan retry berikutnya 409. Rate limit, kuota, atau antrean penuh memberi 429 tanpa perubahan status. Pemilik lain mendapat `not_found`. Isi jawaban tidak berubah.
    - _Requirements: 25.2, 25.3, 25.4, 25.5, 25.6, 25.7, 25.8_

- [ ] 17. Free-text evaluation dan siklus Misconception
  - [~] 17.1 Implementasi handler AITask `answer_evaluation` beserta penyimpanan
    - Jalankan Evaluator (12.4) dan simpan `answer_evaluations` serta `answer_evaluation_misconceptions` secara idempoten lewat unique index sumber, dengan `prompt_contract_id`, `model_name`, `params_snapshot`, `raw_output` terverifikasi (teks Learner dipotong 300 karakter pada evidence).
    - Terapkan `ApplyEvaluationEvent`: hasil sukses (`eval_ok`) menjadikan jawaban `scored`. AITask `failed` (`eval_failed`) menjadikan jawaban `failed` dengan `evaluation_failed_at` pada kegagalan pertama, atau `unscored` bila `evaluation_retry_count` sudah mencapai `ai.max_evaluation_retries`. Hasil yang datang terlambat pada jawaban yang sudah `scored` atau `unscored` dibuang tanpa menulis `answer_evaluations`.
    - Evaluasi `failed` atau `unscored` tidak mengubah `mastery_records` dan `user_misconceptions`. Penyelesaian attempt dihubungkan pada tugas 18.6.
    - _Requirements: 13.12, 14.6, 14.8, 14.9, 20.8, 25.1, 25.8, 25.10_

  - [~] 17.2 Implementasi `ApplyMisconceptionSignals` pada paket `misconception`
    - Buat, buka kembali, atau pertahankan catatan `detected`. Ubah ke `resolved` ketika laporan `resolved_misconception_codes` berturut-turut mencapai `resolve_consecutive_required` (default 1). `severity` dibaca dari database.
    - Evaluasi berstatus `pending`, `failed`, atau `unscored` tidak mengubah state. Catatan `detected` untuk Critical_Misconception nonaktif tetap dihitung sebagai Unresolved_Critical_Misconception.
    - _Requirements: 13.6, 13.7, 13.9, 13.12, 25.10_

  - [~] 17.3 Write property test untuk siklus Misconception
    - **Property 14: Siklus Misconception mengikuti model referensi**
    - **Kategori:** Wajib (area 2: Mastery v1 dan siklus Misconception). Nama test `TestPropW14_<Judul>`.
    - **Validates: Requirements 13.6, 13.7, 13.9, 13.12, 25.10, 26.2**

  - [ ]* 17.4 Write unit test untuk kegagalan evaluasi
    - Provider tidak tersedia dan output gagal validasi: evaluasi berstatus `failed`, mastery tidak berubah, `code` di luar input tidak tersimpan ke `user_misconceptions`, hasil terlambat pada jawaban `unscored` dibuang.
    - _Requirements: 13.12, 14.5, 14.6, 25.8_

- [ ] 18. Mastery v1
  - [~] 18.1 Implementasi `ComputeMastery` sebagai fungsi murni
    - `internal/evaluation/mastery.go`: `MASTERED` bila Attempt sedikitnya 3, rata-rata 3 terakhir sedikitnya `mastered_threshold` (80), tanpa Unresolved_Critical_Misconception, dan sedikitnya satu Attempt aplikatif pada jendela (`application_evidence_scope`, default `window`).
    - Status lain: `NOT_STARTED`, `IN_PROGRESS`, `PRACTICING`, `NEEDS_REVIEW` (`needs_review_floor` default 50, `ever_mastered` monoton, aturan sementara D-6). `evidence` memuat `attempt_ids`, `score_window`, flag Misconception, `blocking_reasons`, dan `parameters` (snapshot nilai parameter yang dipakai, semuanya Initial_Value kecuali diubah Admin). Urutan Attempt berdasarkan `(scored_at, id)`.
    - _Requirements: 12.4, 13.1, 13.2, 13.3, 13.4, 13.5, 13.10, 13.13, 24.7, 26.4_

  - [~] 18.2 Write property test untuk karakterisasi MASTERED
    - **Property 15: MASTERED jika dan hanya jika keempat syarat terpenuhi**
    - **Kategori:** Wajib (area 2: Mastery v1). Nama test `TestPropW15_<Judul>`.
    - **Validates: Requirements 12.4, 13.2, 13.3, 13.5, 26.2**

  - [~] 18.3 Write property test untuk determinisme Mastery
    - **Property 16: Mastery deterministik, idempoten, dan status lain mengikuti tabel keputusan**
    - **Kategori:** Wajib (area 2: Mastery v1). Nama test `TestPropW16_<Judul>`.
    - **Validates: Requirements 13.1, 13.4, 13.10, 13.13, 24.7, 26.2**

  - [~] 18.4 Implementasi `AggregateAssessment` dan `RecordAttempt`
    - `AggregateAssessment` (fungsi murni): hasil `deferred` bila ada jawaban `pending` atau `failed`. Hasil `unscored` bila seluruh jawaban non-kosong `unscored` (tanpa skor dan tanpa `topic_attempts`). Selain itu `scored` dengan rata-rata berbobot `weight` pertanyaan hanya atas jawaban `scored` (termasuk jawaban kosong yang `scored` dengan skor `clamp_min`), jawaban `unscored` dibuang dan tidak memengaruhi skor maupun `has_application_question`.
    - `RecordAttempt` dalam satu transaksi: simpan `topic_attempts`, terapkan `ApplyMisconceptionSignals`, hitung ulang dan simpan `mastery_records` (`rule_version`, `evidence.parameters`).
    - _Requirements: 12.1, 12.2, 12.6, 20.10, 20.11, 20.12, 25.10, 26.4_

  - [~] 18.5 Write property test untuk agregasi Attempt
    - **Property 13: Agregasi Attempt berada di antara skor terkecil dan terbesar yang disertakan**
    - **Kategori:** Wajib (area 1: scoring free-text). Nama test `TestPropW13_<Judul>`.
    - Bagian assessment (`AggregateAssessment`). Bagian interview ditambahkan pada tugas 23.3.
    - **Validates: Requirements 20.10, 21.8, 26.2**

  - [~] 18.6 Hubungkan finalisasi attempt, `RecomputeMastery`, dan resolusi manual Admin
    - `TryFinalizeAssessment` (satu transaksi, attempt dikunci `FOR UPDATE`, hanya bila `submitted`): `deferred` tidak melakukan apa pun (attempt tetap `submitted`), `unscored` mengisi `status = unscored` dan `resolved_at` tanpa `topic_attempts` serta tanpa perubahan mastery atau Misconception, `scored` mengisi `score_pct`, `has_application_question`, `scored_at`, `resolved_at` lalu memanggil `RecordAttempt`.
    - Dipanggil setelah setiap jawaban menjadi `scored` atau `unscored` (handler 17.1) dan saat submit bila seluruh jawaban kosong. Hasil attempt tersedia pada endpoint hasil (16.1).
    - `POST /admin/user-misconceptions/{id}/resolve` (alasan wajib, catat `resolved_by`) lalu `RecomputeMastery` untuk Topic tersebut. Endpoint progress (6.1) kini menampilkan data nyata.
    - _Requirements: 12.6, 12.7, 13.8, 20.10, 20.11, 20.12, 25.10_

  - [ ]* 18.7 Write unit dan integration test untuk Mastery
    - Tabel keputusan dan contoh numerik yang mencakup setiap status, Misconception `minor` tidak memblokir `MASTERED`, injeksi kegagalan pada langkah terakhir `RecordAttempt` membatalkan seluruh perubahan.
    - _Requirements: 12.6, 13.10, 13.11_

  - [~] 18.8 Write property test untuk skor Attempt dari jawaban scored
    - **Property 28: Skor Attempt hanya dari jawaban scored dan unscored tidak berpengaruh**
    - **Kategori:** Wajib (area 1: scoring free-text). Nama test `TestPropW28_<Judul>`.
    - Bagian assessment: `AggregateAssessment` mengembalikan `deferred`, `unscored`, atau `scored` sesuai status jawaban, menambah atau menghapus jawaban `unscored` tidak mengubah `score_pct`, dan `RecordAttempt` pada urutan evaluasi yang hanya berisi `pending`, `failed`, atau `unscored` tidak mengubah `mastery_records` maupun `user_misconceptions`. Bagian interview ditambahkan pada tugas 23.8.
    - **Validates: Requirements 20.10, 20.11, 20.12, 25.10, 13.12, 26.2**

  - [~] 18.9 Implementasi job `UnscoreExpiredFailedAnswers`
    - Job worker (jadwal tiap 15 menit): jawaban pada `assessment_answers` dan `interview_messages` dengan `evaluation_status = 'failed'` dan `evaluation_failed_at <= now - ai.evaluation_retry_window_hours` (Initial_Value 24 jam) menjadi `unscored` lewat `ApplyEvaluationEvent(window_tick)`. `UPDATE ... WHERE evaluation_status = 'failed' AND ... RETURNING` membuat job dan Evaluation_Retry bersamaan tidak saling menimpa.
    - Setelah itu memanggil `TryFinalizeAssessment` untuk attempt terkait dan callback finalisasi sesi interview (`TryRecordInterviewAttempts`, didaftarkan pada tugas 23.4).
    - _Requirements: 20.12, 25.9, 25.10_

  - [ ]* 18.10 Write integration test untuk unscore 24 jam dan finalisasi assessment
    - Dengan jam palsu: jawaban `failed` melewati 24 jam menjadi `unscored`. Attempt `submitted` ditunda lalu `scored` dari jawaban `scored` saja. Bila seluruh jawaban non-kosong `unscored`, attempt menjadi `unscored` tanpa `topic_attempts` dan tanpa perubahan mastery. Job dan Evaluation_Retry bersamaan: salah satu menang.
    - _Requirements: 20.10, 20.11, 20.12, 25.9, 25.10_

- [ ] 19. Rekomendasi rule-based
  - [~] 19.1 Implementasi `Recommend` sebagai fungsi murni
    - `internal/evaluation/recommend.go`: prioritas (1) prerequisite belum `MASTERED`, (2) mastery rendah, (3) relevan dengan Roadmap Learner, (4) urutan kurikulum. Satu rekomendasi per Topic dengan rule berprioritas tertinggi, hanya Topic ber-Published_Translation dan belum `MASTERED`, hasil deterministik dan terpotong oleh `limit`.
    - _Requirements: 22.1, 22.3, 22.4, 22.5, 26.4_

  - [~] 19.2 Write property test untuk rule engine rekomendasi
    - **Property 19: Rule engine rekomendasi deterministik, terurut menurut prioritas, dan menampilkan rule penyebab**
    - **Kategori:** Wajib (area 5: rekomendasi). Nama test `TestPropW19_<Judul>`.
    - **Validates: Requirements 22.1, 22.2, 22.3, 22.4, 22.5, 26.2**

  - [~] 19.3 Implementasi endpoint rekomendasi dan Roadmap Learner
    - `GET /me/recommendations` (`locale`, `limit`) dengan field `rule{code, priority, params}` dan `action` (`study_material`, `take_assessment`, `review`).
    - `PUT`, `DELETE /me/roadmaps/{roadmap_id}` (tabel `user_roadmaps`).
    - _Requirements: 10.13, 22.2, 22.3, 22.6_

  - [ ]* 19.4 Write unit test contoh rekomendasi
    - Dua contoh numerik pada design 4.7 (termasuk kasus frontier prasyarat dan Topic di luar Roadmap).
    - _Requirements: 22.1, 22.4_

- [~] 20. Checkpoint Phase 3
  - Ensure all tests pass, ask the user if questions arise.
  - Jalankan juga seluruh property wajib hingga Phase 3 (Property 3, 5, 11, 12, 13, 14, 15, 16, 19, 20, 21, 22, 27, 28, 29): `go test -race -rapid.checks=100 -run '^TestPropW' ./...`.

### Phase 4: Interview engine

- [ ] 21. Lifecycle Interview_Session
  - [~] 21.1 Implementasi profil dan pembuatan sesi
    - `GET /interview-profiles`. `POST /interviews`: salin profil ke `configuration` (snapshot termasuk bobot), simpan `prompt_contract_versions` aktif untuk setiap Task_Type interview, pilih pertanyaan utama dari bank Question `published` (`purpose` `interview` atau `both`, boleh lintas Topic), isi `locale` dan `retry_of_session_id` bila ada. Sesi baru tanpa batas jumlah dan jeda.
    - Jumlah Topic harus berada di antara `interview.min_topics_per_session` dan `interview.max_topics_per_session` (App_Settings, Initial_Value 1 dan 5, batas sementara D-11). Di luar rentang menghasilkan 400 `validation_error`.
    - _Requirements: 11.1, 11.9, 15.6, 21.1, 21.2, 21.10_

  - [~] 21.2 Implementasi state machine sesi dan endpoint lifecycle
    - `Transition` murni (tabel transisi valid), `start`, `pause` (`paused_at` dan `expires_at` = +7 hari, state dipertahankan), `resume` (409 `session_expired` bila lewat, perubahan ke `ABANDONED` harus di-commit sebelum error dikembalikan), `finish` (202, syarat sedikitnya satu pertanyaan utama terjawab, selain itu 409 `conflict`; mengisi `state.finish_requested` bila `phase` masih `evaluating` atau `generating_followup`), `abandon`, `GET /interviews`, `GET /interviews/{id}`, `GET /interviews/{id}/messages` (cursor).
    - Saat sesi menjadi `ABANDONED`, jawaban `pending` atau `failed` menjadi `unscored` lewat `ApplyEvaluationEvent(session_closed)` (DA-21).
    - Transisi tidak valid menghasilkan 409 `invalid_session_transition`. Jawaban saat status bukan `IN_PROGRESS` menghasilkan `session_not_active`. Hanya pemilik yang dapat mengakses, sesi `deleted_at` terisi dikecualikan.
    - _Requirements: 10.3, 11.2, 11.3, 11.4, 11.6, 11.7, 11.8, 11.10, 11.11, 11.14, 26.4_

  - [~] 21.3 Write property test untuk transisi sesi
    - **Property 8: Transisi status Interview_Session hanya yang valid**
    - **Kategori:** Wajib (area 7: aturan status dan lifecycle, Interview_Session). Nama test `TestPropW8_<Judul>`.
    - **Validates: Requirements 11.2, 11.3, 11.4, 11.11, 26.2**

  - [~] 21.4 Write property test untuk pause dan resume
    - **Property 9: Pause dan resume menjaga state dan batas 7 hari**
    - **Kategori:** Wajib (area 7: aturan status dan lifecycle, Interview_Session). Nama test `TestPropW9_<Judul>`.
    - Memakai Clock palsu. Mencakup `AbandonExpiredSessions`.
    - **Validates: Requirements 11.6, 11.7, 11.8, 26.2**

  - [~] 21.5 Implementasi job terjadwal pada worker
    - `AbandonExpiredSessions` (sesi `PAUSED` dengan `expires_at` terlewati menjadi `ABANDONED`, isi `abandoned_at`, dan jawaban `pending` atau `failed` menjadi `unscored` lewat `session_closed`) dan `PurgeRetention` (isi `deleted_at` setelah `retention_until`). Transcript disimpan tanpa batas bila `retention_until` null.
    - _Requirements: 11.8, 11.12, 11.13_

  - [ ]* 21.6 Write integration test untuk expiry dan retention
    - Job `AbandonExpiredSessions` dan `PurgeRetention` dengan jam palsu, resume sebelum job berjalan menghasilkan `session_expired`.
    - _Requirements: 11.8, 11.12, 11.14_

- [ ] 22. Follow-up allocation dan alur jawaban
  - [~] 22.1 Implementasi `AllocateFollowUp` dan `NextStep` sebagai fungsi murni
    - `internal/evaluation/interview.go`: `total_cap` mengikat, follow-up per pertanyaan tidak melebihi `max_followups_per_question`, depth tidak melebihi `max_depth`, anggaran pertanyaan utama yang tersisa tidak dihabiskan follow-up. Keputusan hanya dari skor, Concept `required` yang terlewat, Misconception terdeteksi, dan state, bukan dari teks AI.
    - Jawaban yang evaluasinya `failed` tidak menghasilkan follow-up, sesi berlanjut ke pertanyaan utama berikutnya.
    - _Requirements: 21.3, 21.4, 21.5, 21.12, 26.4_

  - [~] 22.2 Write property test untuk alokasi follow-up
    - **Property 17: Alokasi follow-up tidak melanggar batas interview dan tidak menghabiskan pertanyaan utama**
    - **Kategori:** Wajib (area 3: alokasi follow-up). Nama test `TestPropW17_<Judul>`.
    - Simulasi sesi penuh dengan kegagalan evaluasi dan kegagalan pembuatan follow-up.
    - **Validates: Requirements 21.2, 21.3, 21.4, 21.5, 26.2**

  - [~] 22.3 Implementasi endpoint jawaban dan orchestrator `OnAnswerEvaluated`
    - `POST /interviews/{id}/answers` (202): kunci sesi, validasi `phase` dan `message_id` (`stale_question`), tolak jawaban kosong atau hanya spasi (`validation_error`), simpan pesan `answer` dengan `evaluation_status = pending` dan `evaluation_prompt_contract_id` dari versi tersimpan pada sesi, cek kuota dan batas antrean, enqueue `answer_evaluation` priority 1 dalam satu transaksi.
    - Handler hasil evaluasi: simpan `answer_evaluations`, panggil `AllocateFollowUp`, enqueue `interview_followup` (kegagalan membuat follow-up berdegradasi ke pertanyaan utama berikutnya), tulis pesan `follow_up` atau pertanyaan utama berikutnya, perbarui `session_state`. Evaluasi jawaban yang `failed` menjadikan jawaban `failed` (atau `unscored` bila retry habis) dan sesi berlanjut ke pertanyaan utama berikutnya tanpa follow-up. Sesi `PAUSED` menyimpan hasil tanpa mengubah status. Sesi `COMPLETED` hanya menambah skor Attempt tanpa follow-up. Sesi `ABANDONED` membuang hasil (`session_closed`).
    - Saat pertanyaan utama habis atau `finish` diminta, panggil interface `SessionFinalizer` yang diimplementasikan pada tugas 23.4.
    - _Requirements: 11.5, 11.11, 15.9, 16.12, 21.4, 21.5, 21.12, 25.1_

  - [ ]* 22.4 Write unit test untuk alur jawaban
    - `stale_question`, kuota habis tidak menyimpan jawaban, follow-up gagal berdegradasi, jawaban `failed` melanjutkan ke pertanyaan utama berikutnya tanpa follow-up, hasil dibuang pada sesi yang sudah ditutup.
    - _Requirements: 11.11, 16.4, 21.4, 21.12_

- [ ] 23. Interview evaluation dan report
  - [~] 23.1 Implementasi `ScoreInterview` dan `AggregateInterviewTopic`
    - `ScoreInterview`: `overall_score = round2(sum(skor_i * bobot_i) / 100)` atas lima dimensi (Technical Accuracy, Conceptual Understanding, Reasoning, Practical Application, Communication) dengan skor di-clamp 0 sampai 100 dan bobot dari snapshot `configuration.weights`. Dimensi hilang menghasilkan error.
    - `AggregateInterviewTopic`: rata-rata berbobot skor jawaban `scored` per Topic (pertanyaan utama dan follow-up digabung per pertanyaan utama), `has_application_question` per Topic, dan hasil `null` bila seluruh jawaban Topic tidak `scored`. Jawaban `unscored` dan `failed` tidak memengaruhi skor rantai yang memiliki jawaban `scored` lain.
    - Keduanya fungsi murni (Requirement 26.4).
    - _Requirements: 21.6, 21.7, 21.8, 21.11, 26.4_

  - [~] 23.2 Write property test untuk skor interview
    - **Property 18: Skor interview adalah rata-rata berbobot yang di-clamp**
    - **Kategori:** Wajib (area 4: skor interview). Nama test `TestPropW18_<Judul>`.
    - **Validates: Requirements 21.6, 21.7, 26.2**

  - [~] 23.3 Write property test untuk agregasi per Topic pada interview
    - **Property 13: Agregasi Attempt berada di antara skor terkecil dan terbesar yang disertakan**
    - **Kategori:** Wajib (area 1: scoring free-text). Nama test `TestPropW13_<Judul>` (bagian interview).
    - Bagian `AggregateInterviewTopic` (hanya pertanyaan Topic tersebut yang disertakan).
    - **Validates: Requirements 20.10, 21.8, 26.2**

  - [~] 23.4 Implementasi finalisasi sesi dan handler `interview_evaluation`
    - `SessionFinalizer`: ubah status `COMPLETED` tanpa menunggu `interview_evaluation` maupun retry jawaban (DA-10), enqueue `interview_evaluation` (priority 2), lalu panggil `TryRecordInterviewAttempts`. Sesi `ABANDONED` tidak membuat Attempt.
    - `TryRecordInterviewAttempts` (idempoten, hanya untuk sesi `COMPLETED`): tunda bila ada jawaban sesi `pending` atau `failed`. Bila tidak, panggil `RecordAttempt` satu kali per Topic yang tercakup, kecuali Topic yang seluruh jawabannya `unscored` (tanpa `topic_attempts`, mastery tidak berubah). Dipanggil dari finalisasi, handler hasil evaluasi, dan job `UnscoreExpiredFailedAnswers` (didaftarkan sebagai callback pada tugas 18.9).
    - Handler `interview_evaluation`: validasi output (`additionalProperties: false`, tanpa `overall_score`), hitung skor di server dengan `ScoreInterview`, simpan `interview_evaluations` (`status`, `dimension_scores`, `weights`, `prompt_contract_id`, `model_name`, `raw_output`). `overall_score` tidak menjadi input `score_pct` Attempt maupun Mastery v1. Kegagalan menandai `failed` tanpa memengaruhi Attempt. `POST /interviews/{id}/evaluation/retry` (202) untuk evaluasi dimensi `failed`.
    - _Requirements: 11.15, 21.7, 21.8, 21.9, 21.11, 25.10_

  - [~] 23.5 Implementasi report interview
    - `GET /interviews/{id}/evaluation`: `overall_score`, `dimension_scores`, `weights`, `summary`, skor per Topic, status Mastery terbaru Topic yang tercakup, dan status evaluasi (`pending`, `succeeded`, `failed`). Hasil interview menampilkan `evaluation_status` dan `evaluation_retry_count` setiap jawaban. Hanya pemilik, `not_found` untuk pihak lain.
    - _Requirements: 10.3, 11.15, 21.6, 21.7, 21.8, 25.11_

  - [ ]* 23.6 Write integration test alur interview penuh
    - Dengan provider replay: create, start, jawab, follow-up, pause, resume, finish, evaluasi, report, dan perubahan mastery. Termasuk provider 429 lalu fallback dan pinning versi Prompt_Contract setelah versi aktif diganti.
    - _Requirements: 11.6, 11.7, 15.6, 16.8, 16.9, 21.8_

  - [~] 23.7 Implementasi Evaluation_Retry untuk jawaban interview
    - `POST /interviews/{session_id}/messages/{message_id}/evaluation/retry` (pesan `kind = answer`), hanya pemilik. Memakai prosedur retry yang sama dengan assessment (tugas 16.4): `FOR UPDATE`, `ApplyEvaluationEvent(retry_requested)`, Prompt_Contract dari versi tersimpan pada sesi, AITask `answer_evaluation` priority 1, `idempotency_key` `eval-retry:{message_id}:{retry_count}`.
    - 202 bila AITask baru dibuat, 200 bila `pending` atau `scored`, 409 `evaluation_not_retryable` untuk `unscored` atau retry habis, `session_not_active` untuk sesi `ABANDONED`, dan 429 (`rate_limited`, `ai_quota_exceeded`, `ai_queue_limit_exceeded`) tanpa mengubah status. Sesi `COMPLETED` boleh di-retry dan hasilnya hanya menambah skor Attempt tanpa follow-up (lanjut ke `TryRecordInterviewAttempts`). Isi jawaban tidak berubah.
    - _Requirements: 10.14, 21.12, 25.2, 25.3, 25.4, 25.5, 25.6, 25.7_

  - [~] 23.8 Write property test untuk skor Attempt interview dari jawaban scored
    - **Property 28: Skor Attempt hanya dari jawaban scored dan unscored tidak berpengaruh**
    - **Kategori:** Wajib (area 1: scoring free-text). Nama test `TestPropW28_<Judul>` (bagian interview).
    - Bagian interview: `AggregateInterviewTopic` bernilai `null` bila seluruh jawaban Topic bukan `scored`, dan jawaban `unscored` atau `failed` pada sebuah rantai tidak mengubah skor rantai yang memiliki jawaban `scored` lain.
    - **Validates: Requirements 20.10, 21.8, 21.11, 25.10, 26.2**

  - [ ]* 23.9 Write integration test untuk Evaluation_Retry interview dan unscored
    - Retry jawaban interview (202, 200 idempoten, 409, `session_not_active` pada sesi `ABANDONED`). Dengan jam palsu: sesi `COMPLETED` menunda `topic_attempts` selama ada jawaban `failed`, lalu mencatat Attempt setelah `scored` atau `unscored`. Topic yang seluruh jawabannya `unscored` tidak menghasilkan `topic_attempts`.
    - _Requirements: 21.11, 21.12, 25.2, 25.5, 25.6, 25.9, 25.10_

- [ ] 24. Finalisasi lintas fase
  - [~] 24.1 Tambahkan contract test untuk contoh JSON dan DTO
    - `santhosh-tekuri/jsonschema` dengan fixture di `test/fixtures`: contoh request dan response API, contoh Prompt_Contract valid dan tidak valid, serta kesesuaian nama field `snake_case` dengan DTO.
    - Setiap endpoint terdokumentasi dengan method, path, role, dan kode error, termasuk endpoint Evaluation_Retry, `/admin/settings`, dan `/admin/ai-quota-settings`. Response hasil assessment dan interview memuat `evaluation_status` dan `evaluation_retry_count`, response App_Settings memuat `is_initial_value`.
    - _Requirements: 1.4, 1.5, 1.7, 9.10, 10.14, 10.15, 24.4, 25.11_

  - [~] 24.2 Konfigurasi pipeline CI dan gerbang property wajib
    - `go vet`, `staticcheck`, `gofmt`, unit test, integration test, contract test, pemeriksaan migrasi (up, down, up, diff schema).
    - Property wajib (Requirement 26.1, 26.2) menjadi gerbang merge: `go test -race -rapid.checks=100 -run '^TestPropW' ./...`, kegagalan memblokir merge. Property opsional (`-run '^TestPropO'`) berjalan pada job terpisah yang melaporkan kegagalan tanpa memblokir merge.
    - Skrip pemeriksa memastikan test `TestPropW<N>_*` ada untuk setiap N berkategori Wajib (3, 5, 8, 9, 11, 12, 13, 14, 15, 16, 17, 18, 19, 20, 21, 22, 27, 28, 29), sehingga property wajib tidak dapat terlewat dari gerbang.
    - Run Golden_Set mode `recorded` (provider replay, tanpa AI provider) ada di pipeline CI. Golden_Set `live` tidak masuk pipeline CI dan dipicu manual atau oleh jadwal worker. Job nightly (di luar gerbang merge): property wajib dan opsional 1000 iterasi, fuzz 5 menit per target.
    - Tag komentar `// Feature: backend-lab-technical-specs, Property N: <judul>` dan `// Kategori: Wajib` atau `// Kategori: Opsional` pada seluruh property test.
    - _Requirements: 2.6, 2.7, 17.9, 17.12, 17.13, 26.2, 26.3, 26.5_

- [~] 25. Checkpoint Phase 4
  - Ensure all tests pass, ask the user if questions arise.
  - Pastikan seluruh property wajib lulus sebagai gerbang CI (Property 3, 5, 8, 9, 11 sampai 22, 27, 28, 29): `go test -race -rapid.checks=100 -run '^TestPropW' ./...`.

### Opsional (P1): RAG/pgvector

- [ ] 26. RAG/pgvector (P1, opsional)
  Seluruh sub-tugas di bagian ini opsional. Alur P0 tidak bergantung padanya.

  - [ ]* 26.1 Tulis migrasi `0100_p1_pgvector_rag` dan gating `FEATURE_RAG`
    - `CREATE EXTENSION vector`, `material_chunks` dengan indeks HNSW, seed Task_Type `embedding_generation`. Hanya dijalankan dengan `cmd/migrate up --include-p1` dan `FEATURE_RAG=true`. Tidak ada tabel P0 yang merujuk tabel P1.
    - _Requirements: 2.6, 19.2_

  - [ ]* 26.2 Implementasi chunking dan handler `embedding_generation`
    - Hanya Translation `published` yang di-chunk. Tulis `material_chunks` secara idempoten (`content_sha256`) sebagai background task priority 3.
    - _Requirements: 5.4, 19.2_

  - [ ]* 26.3 Implementasi repository retrieval berbasis cosine
    - Query kemiripan hanya atas chunk dari Translation `published` pada Locale yang diminta. Belum ada endpoint publik (AI tutoring adalah titik ekstensi P2).
    - _Requirements: 5.4, 5.8, 19.3_

  - [ ]* 26.4 Write integration test migrasi P1
    - Migrasi `0001` sampai `0009` berjalan pada image PostgreSQL tanpa pgvector. `0100` berjalan pada image dengan pgvector.
    - _Requirements: 2.6, 2.7, 19.2_

## Notes

- Tugas bertanda `*` bersifat opsional dan dapat dilewati untuk MVP yang lebih cepat. Pengecualian: seluruh property test pada delapan area logika inti (Requirement 26.1, D-16) **wajib**, tidak bertanda `*`, dan harus diimplementasikan.
- Property wajib (kategori pada design): 3, 5, 8, 9, 11, 12, 13, 14, 15, 16, 17, 18, 19, 20, 21, 22, 27, 28, 29. Property 13 dan 28 masing-masing dibangun dalam dua tahap pada satu fungsi test yang sama (bagian assessment pada 18.5 dan 18.8, bagian interview ditambahkan pada 23.3 dan 23.8).
- Property opsional (bertanda `*`): 1, 2, 4, 6, 7, 10, 23, 24, 25, 26, 30, 31.
- Nama test property: `TestPropW<N>_<Judul>` untuk wajib dan `TestPropO<N>_<Judul>` untuk opsional, agar CI dapat memilih dengan `-run '^TestPropW'`. Property wajib menjadi gerbang merge CI (tugas 24.2). Setiap checkpoint menjalankan property wajib yang sudah ada.
- Fungsi pada delapan area inti ditulis sebagai fungsi murni tanpa jaringan, database, atau jam sistem (Requirement 26.4). Waktu, acak, dan parameter `P` adalah argumen.
- Seluruh migrasi P0 (`0001` sampai `0009`) dikerjakan pada Phase 1 agar skema dan verifikasi CI lengkap sejak awal. Fase berikutnya hanya memakai tabel yang sudah ada. Tidak ada tabel `ai_quota_settings`: kuota AI dan seluruh parameter lain berada pada `app_settings` sebagai Initial_Value, bukan nilai final produk.
- Service App_Settings (tugas 1.6) dibuat sebelum auth, quota, scoring, dan mastery karena TTL token, kuota, dan parameter dibaca dari sana. Endpoint `/admin/settings` dan `/admin/ai-quota-settings` ada pada tugas 14.4.
- Rate limiter HTTP (tugas 9) dan antrean AITask (tugas 10) dibuat pada Phase 2. Sampai tugas 9.2 selesai, endpoint auth belum dibatasi laju. Penyimpanan queue dan rate limit dipilih lewat `DEPLOYMENT_MODE`, `RATELIMIT_STORE`, `QUEUE_STORE` (default PostgreSQL untuk multi-instance, `memory` hanya single-instance atau dev).
- Penyelesaian attempt assessment dihubungkan pada 18.6 dan penyelesaian sesi interview pada 23.4. Interface yang dideklarasikan lebih awal (17.1 dan 22.3) tidak berfungsi penuh sampai tugas tersebut selesai. Job `UnscoreExpiredFailedAnswers` (18.9) memanggil callback interview yang didaftarkan pada 23.4.
- Evaluation_Retry untuk assessment (16.4) dan interview (23.7) memakai aturan yang sama dari `ApplyEvaluationEvent` (16.2). Jawaban `unscored` tidak ikut skor Attempt dan tidak mengubah mastery maupun Misconception.
- Golden_Set mode `recorded` (CI, tanpa AI provider) dan `live` (manual atau jadwal berkala di luar CI) dibuat pada 13.5. Hanya run `live` yang menjadi baseline regresi dan syarat aktivasi Prompt_Contract.
- Requirement 1 (struktur Spec_Set, traceability, bagian Open Decisions) dan Requirement 19.1, 19.3, 19.4 bersifat dokumentasi. Hanya aspek yang dapat diuji kode yang dirujuk (24.1). Titik ekstensi P2 tidak diimplementasikan.
- Provider AI nyata hanya dipakai pada gate aktivasi, run Golden_Set `live`, dan job nightly. CI memakai `provider/replay`.
- Checkpoint memastikan validasi inkremental di akhir setiap fase.

## Task Dependency Graph

```json
{
  "waves": [
    { "id": 0, "tasks": ["1.1"] },
    { "id": 1, "tasks": ["1.2", "1.4", "2.1"] },
    { "id": 2, "tasks": ["1.3", "1.5", "2.2", "3.1"] },
    { "id": 3, "tasks": ["2.3", "4.1", "4.3"] },
    { "id": 4, "tasks": ["1.6", "2.4", "4.2", "4.4"] },
    { "id": 5, "tasks": ["1.7", "2.5", "3.2"] },
    { "id": 6, "tasks": ["2.6", "3.3", "4.5", "5.1", "6.1"] },
    { "id": 7, "tasks": ["2.7", "3.4", "3.5", "4.6", "5.2", "5.3", "6.2"] },
    { "id": 8, "tasks": ["3.6", "3.7", "3.10", "5.4"] },
    { "id": 9, "tasks": ["3.8", "3.9", "3.11"] },
    { "id": 10, "tasks": ["7"] },
    { "id": 11, "tasks": ["8.1", "9.1", "10.1", "11.1", "12.1"] },
    { "id": 12, "tasks": ["8.2", "9.2", "9.3", "10.2", "10.3", "11.2", "11.4", "12.2", "12.3", "13.1", "14.4"] },
    { "id": 13, "tasks": ["8.3", "9.4", "10.4", "10.5", "10.6", "11.3", "11.5", "11.6", "13.2", "14.1"] },
    { "id": 14, "tasks": ["10.7", "11.7", "12.4", "13.3", "14.5"] },
    { "id": 15, "tasks": ["13.4", "13.5"] },
    { "id": 16, "tasks": ["13.7", "14.2"] },
    { "id": 17, "tasks": ["13.6", "14.3"] },
    { "id": 18, "tasks": ["15"] },
    { "id": 19, "tasks": ["16.1", "16.2", "17.2", "18.1", "19.1"] },
    { "id": 20, "tasks": ["16.3", "17.3", "18.2", "18.3", "18.4", "19.2", "19.3", "19.4"] },
    { "id": 21, "tasks": ["16.4", "16.6", "17.1", "18.5", "18.8"] },
    { "id": 22, "tasks": ["16.5", "16.7", "17.4", "18.6"] },
    { "id": 23, "tasks": ["18.7", "18.9"] },
    { "id": 24, "tasks": ["18.10"] },
    { "id": 25, "tasks": ["20"] },
    { "id": 26, "tasks": ["21.1", "22.1", "23.1"] },
    { "id": 27, "tasks": ["21.2", "22.2", "23.2", "23.3", "23.8"] },
    { "id": 28, "tasks": ["21.3", "21.5", "22.3"] },
    { "id": 29, "tasks": ["21.4", "21.6", "22.4", "23.4"] },
    { "id": 30, "tasks": ["23.5", "23.7"] },
    { "id": 31, "tasks": ["23.6", "23.9", "24.1"] },
    { "id": 32, "tasks": ["24.2"] },
    { "id": 33, "tasks": ["25"] },
    { "id": 34, "tasks": ["26.1"] },
    { "id": 35, "tasks": ["26.2", "26.3", "26.4"] }
  ]
}
```
