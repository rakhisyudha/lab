# Requirements Document

## Introduction

Dokumen ini menetapkan requirements untuk empat dokumen teknis turunan dari Product Spec Backend Lab v1.1: (1) database schema, (2) API contracts, (3) AI prompt contracts, dan (4) evaluation algorithms. Setiap requirement di bawah ini mendeskripsikan perilaku sistem yang WAJIB dispesifikasikan secara eksplisit oleh keempat dokumen turunan tersebut. Dokumen turunan ditulis dalam Bahasa Indonesia dengan istilah teknis Inggris.

Format contoh di dokumen turunan: schema dalam SQL PostgreSQL, kontrak API dan prompt dalam JSON, algoritma dalam pseudocode terstruktur. Nama tabel, kolom, dan field JSON memakai Inggris snake_case.

Cakupan: seluruh fitur P0 dispesifikasikan penuh. RAG/pgvector (P1) dispesifikasikan sebagai migrasi terpisah. Fitur P2 hanya disiapkan sebagai titik ekstensi.

Catatan sumber: berkas Product Spec Backend Lab v1.1 tidak tersedia di workspace. Requirements ini bersumber dari keputusan user, ringkasan Product Spec v1.1 yang diberikan pada tahap clarify dan tahap design (role, batas interview, lifecycle interview, Mastery v1, bobot dimensi, AITask, status konten, rekomendasi, anti prompt injection, Golden_Set), dan asumsi yang ditandai eksplisit. Bagian Product Spec yang belum terlihat dicatat sebagai open item OD-1.

## Glossary

- **Backend_Lab**: Sistem backend yang dispesifikasikan. Nama produk ini adalah placeholder (lihat ASUMSI A-1).
- **Spec_Set**: Kumpulan empat dokumen turunan: Database_Schema_Doc, API_Contract_Doc, AI_Prompt_Contract_Doc, Evaluation_Algorithm_Doc.
- **Database_Schema_Doc**: Dokumen turunan yang berisi database schema dalam SQL PostgreSQL.
- **API_Contract_Doc**: Dokumen turunan yang berisi kontrak endpoint HTTP dalam JSON.
- **AI_Prompt_Contract_Doc**: Dokumen turunan yang berisi kontrak input dan output setiap task type AI dalam JSON.
- **Evaluation_Algorithm_Doc**: Dokumen turunan yang berisi algoritma scoring, mastery decision, dan golden set dalam pseudocode terstruktur.
- **API_Server**: Komponen Backend_Lab yang melayani request HTTP di bawah prefix `/api/v1`.
- **Auth_Service**: Komponen Backend_Lab yang menangani registrasi, login, penerbitan token, rotation, dan revocation.
- **Content_Service**: Komponen Backend_Lab yang menyajikan topic, material, dan question kepada Learner.
- **Translation_Service**: Komponen Backend_Lab yang mengelola siklus workflow translation.
- **AI_Gateway**: Komponen Backend_Lab yang memanggil model AI untuk setiap task type, memvalidasi output, dan mencatat penggunaan.
- **Quota_Manager**: Komponen AI_Gateway yang menghitung dan membatasi pemakaian AI per periode.
- **Rate_Limiter**: Komponen yang menerapkan algoritma token bucket pada request.
- **Job_Queue**: Antrean AITask yang berbasis PostgreSQL (default untuk deployment multi-instance) atau berjalan in-process (hanya untuk deployment single-instance atau dev), sesuai KEPUTUSAN D-15.
- **AITask**: Satu unit kerja AI asinkron pada tabel `ai_tasks` dengan status `queued`, `processing`, `completed`, atau `failed`.
- **Evaluator**: Task type AI `answer_evaluation` yang menilai satu jawaban free-text terhadap Concept dan Misconception_Catalog. Task type `interview_evaluation` menilai dimensi interview pada akhir sesi.
- **Mastery_Engine**: Komponen Backend_Lab yang menghitung score Attempt dan menetapkan Mastery_Status berdasarkan hasil Evaluator.
- **Golden_Set_Runner**: Komponen Backend_Lab yang menjalankan Evaluator `answer_evaluation` terhadap Golden_Set.
- **Assessment_Service**: Komponen Backend_Lab yang mengelola assessment attempt.
- **Recommendation_Service**: Komponen Backend_Lab yang menjalankan rule engine rekomendasi.
- **Learner**: User dengan role `user`.
- **Reviewer**: Referensi ke user ber-role `admin` yang tercatat pada kolom `reviewer_id` sebuah Translation. Reviewer bukan role terpisah (lihat KEPUTUSAN D-4 dan ASUMSI A-12).
- **Admin**: User dengan role `admin`.
- **Topic**: Entitas induk konten yang mengelompokkan Material, Question, Concept, dan Misconception.
- **Material**: Entitas induk konten pembelajaran di bawah sebuah Topic.
- **Question**: Entitas induk pertanyaan di bawah sebuah Topic, dengan Translation per Locale dan daftar Concept.
- **Concept**: Unit pengetahuan yang dinilai pada sebuah Question, diidentifikasi `code` per Topic.
- **Locale**: Kode bahasa konten dalam format BCP 47 yang disederhanakan (contoh `id`, `en`).
- **Translation**: Baris pada tabel `*_translations` yang menyimpan field teks satu entitas untuk satu Locale, beserta workflow status, version, dan reviewer.
- **Translation_Status**: Salah satu nilai `draft`, `in_review`, `published`, `archived`.
- **Published_Translation**: Translation dengan Translation_Status `published`.
- **Default_Locale**: Locale yang dikonfigurasi sebagai bahasa utama sistem (lihat ASUMSI A-3).
- **Misconception**: Entri pada tabel `misconceptions` yang dikurasi Admin, berisi `code`, `description`, `severity`, dan `topic_id`.
- **Severity**: Salah satu nilai `minor` atau `critical` pada Misconception.
- **Critical_Misconception**: Misconception dengan `severity` bernilai `critical`. Ini adalah definisi formal "critical".
- **Misconception_Catalog**: Himpunan Misconception aktif milik satu Topic.
- **Unresolved_Critical_Misconception**: Catatan `user_misconceptions` berstatus `detected` untuk seorang Learner yang merujuk Critical_Misconception.
- **Attempt**: Satu baris `topic_attempts` yang mewakili hasil nilai satu assessment attempt atau satu sesi interview pada satu Topic.
- **MasteryRecord**: Baris `mastery_records` per (user, Topic) yang menyimpan Mastery_Status dan evidence.
- **Mastery_Status**: Status penguasaan Learner pada satu Topic, salah satu dari `NOT_STARTED`, `IN_PROGRESS`, `PRACTICING`, `NEEDS_REVIEW`, `MASTERED`.
- **Interview_Type**: Salah satu nilai `practice` atau `recruitment`.
- **Interview_Session**: Satu sesi tanya-jawab AI antara Learner dan AI pada satu atau beberapa Topic, dengan status `CREATED`, `IN_PROGRESS`, `PAUSED`, `COMPLETED`, atau `ABANDONED`.
- **Transcript**: Himpunan `interview_messages` milik satu Interview_Session.
- **Interview_Ready_Flag**: Kolom boolean nullable pada MasteryRecord. Tidak memiliki rumus (lihat ASUMSI A-5).
- **Task_Type**: Pengenal teks terdaftar di registry AI_Prompt_Contract_Doc yang menentukan satu kontrak prompt.
- **Prompt_Contract**: Definisi versi dari satu Task_Type: template, JSON Schema input, JSON Schema output, dan batasan.
- **Golden_Set**: Himpunan kasus uji berlabel milik Admin (pertanyaan, jawaban contoh, rentang skor, Concept, dan Misconception yang diharapkan) untuk memvalidasi Evaluator `answer_evaluation`. Label memakai `code` dari Misconception_Catalog dan Concept.
- **Invite_Code**: Kode sekali pakai yang dibuat Admin dan disimpan pada tabel `invitations`.
- **Access_Token**: JWT berumur pendek untuk otorisasi request.
- **Refresh_Token**: Token opaque berumur panjang untuk menerbitkan Access_Token baru. Disimpan sebagai hash di tabel `refresh_tokens`.
- **Token_Family**: Rantai Refresh_Token yang berasal dari satu login, diidentifikasi `family_id`.
- **Request_ID**: Pengenal unik per request yang dikembalikan di header `X-Request-ID` dan field `request_id` pada error.
- **Error_Envelope**: Bentuk tunggal error API dengan field `code`, `message`, `details`, `request_id`.
- **App_Settings**: Tabel `app_settings` berisi pasangan `key` dan `value` (JSON) untuk parameter scoring, mastery, AI, dan token yang dapat diubah Admin tanpa deploy ulang (lihat Requirement 24).
- **Initial_Value**: Nilai awal sebuah parameter pada App_Settings yang ditetapkan sebagai seed. Initial_Value bukan nilai final produk dan dapat diubah Admin.
- **Answer_Evaluation_Status**: Status evaluasi satu jawaban, salah satu dari `pending`, `scored`, `failed`, `unscored`. Status `failed` dapat di-retry. Status `unscored` bersifat final.
- **Unscored_Answer**: Jawaban Learner yang tersimpan dengan Answer_Evaluation_Status `unscored` dan tidak ikut dalam perhitungan skor Attempt.
- **Evaluation_Retry**: Permintaan Learner untuk mengulang evaluasi sebuah jawaban tersimpan berstatus `failed` tanpa mengubah isi jawaban (lihat Requirement 25).
- **Golden_Run_Mode**: Mode eksekusi Golden_Set_Runner, `recorded` (memakai response model yang direkam) atau `live` (memakai model nyata).
- **Property_Test**: Pengujian berbasis properti yang memeriksa invariant fungsi atau aturan terhadap banyak input yang dibangkitkan (lihat Requirement 26).

## Requirements

### Requirement 1: Struktur dan keterlacakan Spec_Set

**User Story:** As a backend engineer, I want empat dokumen turunan yang konsisten dan saling terlacak, so that implementasi dapat dimulai tanpa menebak keputusan desain.

#### Acceptance Criteria

1. THE Spec_Set SHALL terdiri atas tepat empat dokumen: Database_Schema_Doc, API_Contract_Doc, AI_Prompt_Contract_Doc, dan Evaluation_Algorithm_Doc.
2. THE Spec_Set SHALL menuliskan narasi dalam Bahasa Indonesia dan mempertahankan istilah teknis dalam bahasa Inggris.
3. THE Database_Schema_Doc SHALL menyajikan contoh dalam SQL PostgreSQL.
4. THE API_Contract_Doc SHALL menyajikan contoh request dan response dalam JSON.
5. THE AI_Prompt_Contract_Doc SHALL menyajikan contoh Prompt_Contract dalam JSON.
6. THE Evaluation_Algorithm_Doc SHALL menyajikan algoritma dalam pseudocode terstruktur yang memuat input, output, langkah, dan kondisi terminasi.
7. THE Spec_Set SHALL memakai Inggris snake_case untuk nama tabel, kolom, dan field JSON.
8. THE Spec_Set SHALL memuat tabel traceability yang memetakan setiap requirement pada dokumen ini ke bagian dokumen turunan yang memenuhinya.
9. WHEN sebuah istilah di Glossary dipakai pada dokumen turunan, THE Spec_Set SHALL memakai istilah yang sama tanpa sinonim.
10. THE Spec_Set SHALL mencantumkan bagian "Open Decisions & Assumptions" yang membedakan Keputusan (D-n) dari Asumsi (A-n) dan Open Item (OD-n).

### Requirement 2: Konvensi database umum

**User Story:** As a backend engineer, I want konvensi database yang seragam, so that seluruh tabel konsisten dan mudah dimigrasikan.

#### Acceptance Criteria

1. THE Database_Schema_Doc SHALL mendefinisikan primary key setiap tabel sebagai kolom `id` bertipe `uuid`, dengan UUID v7 sebagai target generasi bila tersedia (lihat ASUMSI A-8).
2. THE Database_Schema_Doc SHALL mendefinisikan setiap kolom waktu sebagai `timestamptz` yang menyimpan nilai UTC.
3. THE Database_Schema_Doc SHALL mendefinisikan kolom `created_at` dan `updated_at` pada setiap tabel yang barisnya dapat diubah.
4. THE Database_Schema_Doc SHALL mendefinisikan foreign key, aksi `ON DELETE`, dan index untuk setiap relasi.
5. THE Database_Schema_Doc SHALL mendefinisikan nilai enumerasi sebagai kolom `text` dengan constraint `CHECK`, sehingga penambahan nilai tidak memerlukan perubahan tipe.
6. THE Database_Schema_Doc SHALL menyertakan urutan migrasi bernomor dengan satu migrasi terpisah untuk ekstensi `pgvector` dan tabel RAG (P1).
7. THE Database_Schema_Doc SHALL menjalankan seluruh migrasi P0 tanpa bergantung pada ekstensi `pgvector` maupun Redis.
8. THE Database_Schema_Doc SHALL mendefinisikan kebijakan soft delete melalui kolom `deleted_at` nullable pada tabel yang mensyaratkannya, dan mendefinisikan partial index yang mengecualikan baris dengan `deleted_at` terisi.

### Requirement 3: Entitas konten bilingual (Keputusan D-2)

**User Story:** As an Admin, I want konten dua bahasa yang terpisah dari metadata, so that setiap Locale dapat dikelola dan ditinjau secara independen.

#### Acceptance Criteria

1. THE Database_Schema_Doc SHALL menyimpan hanya metadata non-teks pada tabel entitas induk (contoh `topics`, `materials`), seperti `slug`, `position`, `topic_id`, dan `is_active`.
2. THE Database_Schema_Doc SHALL menyimpan setiap field teks yang tampil ke Learner pada tabel `*_translations` per Locale (contoh `topic_translations`, `material_translations`).
3. THE Database_Schema_Doc SHALL mendefinisikan unique constraint pada pasangan (id entitas induk, `locale`) di setiap tabel `*_translations`.
4. THE Database_Schema_Doc SHALL mendefinisikan kolom `status`, `version`, `reviewer_id`, `reviewed_at`, dan `published_at` pada setiap tabel `*_translations`.
5. THE Database_Schema_Doc SHALL membatasi `status` pada nilai `draft`, `in_review`, `published`, `archived` melalui constraint `CHECK`.
6. THE Database_Schema_Doc SHALL mendefinisikan `locale` sebagai kolom yang divalidasi terhadap daftar Locale yang didukung (lihat ASUMSI A-3).
7. THE Database_Schema_Doc SHALL menerapkan pola entitas induk dan tabel translations pada setiap entitas konten P0 yang memiliki field teks: Topic, Material, Question, Concept, Misconception, dan Roadmap.
8. THE Database_Schema_Doc SHALL mendefinisikan satu partial unique index sehingga setiap pasangan (entitas, `locale`) memiliki paling banyak satu baris dengan `status` `published`.

### Requirement 4: Workflow, versioning, dan reviewer Translation

**User Story:** As an Admin yang bertindak sebagai Reviewer, I want workflow terjemahan yang jelas per Locale, so that hanya konten yang sudah ditinjau yang dapat dilihat Learner.

#### Acceptance Criteria

1. WHEN sebuah Translation baru dibuat, THE Translation_Service SHALL menetapkan `status` `draft` dan `version` 1 (lihat ASUMSI A-9).
2. WHEN field teks sebuah Translation diubah, THE Translation_Service SHALL menaikkan `version` sebesar 1.
3. THE API_Contract_Doc SHALL mendefinisikan transisi status yang valid: `draft` ke `in_review`, `in_review` ke `draft`, `in_review` ke `published`, `published` ke `archived`, dan `archived` ke `draft`.
4. WHEN sebuah transisi ke `published` diminta, THE Translation_Service SHALL mensyaratkan `reviewer_id` terisi dengan user ber-role `admin`, dan mengisi `reviewed_at` serta `published_at`.
5. IF sebuah transisi status di luar daftar valid diminta, THEN THE Translation_Service SHALL menolak request dengan HTTP 409 dan `code` `invalid_status_transition`.
6. IF sebuah request ubah Translation membawa `version` yang berbeda dari `version` tersimpan, THEN THE Translation_Service SHALL menolak request dengan HTTP 409 dan `code` `version_conflict`.
7. WHEN field teks sebuah Published_Translation diubah, THE Translation_Service SHALL mengembalikan `status` ke `draft` dan menghapus `published_at`.
8. THE Database_Schema_Doc SHALL menyimpan hanya `version` terkini pada baris Translation, tanpa tabel riwayat versi (lihat ASUMSI A-9).

### Requirement 5: Penyajian konten dan aturan fallback Locale

**User Story:** As a Learner, I want konten disajikan dalam bahasa yang saya minta, so that saya hanya melihat konten yang sudah dipublikasikan.

#### Acceptance Criteria

1. THE Content_Service SHALL menentukan Locale permintaan dengan urutan prioritas: query parameter `locale`, header `Accept-Language`, preferensi Locale Learner, lalu Default_Locale.
2. WHEN Locale permintaan berbentuk tag regional yang tidak didukung (contoh `id-ID`), THE Content_Service SHALL memetakan tag tersebut ke tag bahasa dasar (contoh `id`) sebelum memeriksa daftar Locale yang didukung.
3. WHEN Locale permintaan tidak termasuk daftar Locale yang didukung setelah pemetaan, THE Content_Service SHALL memakai Default_Locale sebagai Locale permintaan.
4. THE Content_Service SHALL menyajikan sebuah entitas hanya WHEN entitas tersebut memiliki Published_Translation pada Locale permintaan.
5. WHEN daftar entitas diminta, THE Content_Service SHALL mengecualikan entitas yang tidak memiliki Published_Translation pada Locale permintaan.
6. IF detail sebuah entitas diminta dan entitas tersebut tidak memiliki Published_Translation pada Locale permintaan, THEN THE Content_Service SHALL mengembalikan HTTP 404 dengan `code` `translation_not_available` dan `details.available_locales` berisi Locale yang memiliki Published_Translation.
7. THE Content_Service SHALL menyertakan field `locale` pada setiap response konten yang menyatakan Locale Translation yang disajikan.
8. THE Content_Service SHALL menyajikan field teks hanya dari Translation pada Locale permintaan, tanpa mengganti satu field dengan field dari Locale lain.
9. THE Evaluation_Algorithm_Doc SHALL menyertakan pseudocode resolusi Locale dan filter Published_Translation sesuai kriteria 1 sampai 8.

### Requirement 6: Katalog Misconception (Keputusan D-1)

**User Story:** As an Admin, I want katalog miskonsepsi per Topic yang saya kurasi, so that evaluasi AI konsisten dan dapat diaudit.

#### Acceptance Criteria

1. THE Database_Schema_Doc SHALL mendefinisikan tabel `misconceptions` dengan kolom `id`, `topic_id`, `code`, `severity`, `is_active`, `created_at`, `updated_at`.
2. THE Database_Schema_Doc SHALL membatasi `severity` pada nilai `minor` dan `critical` melalui constraint `CHECK`.
3. THE Database_Schema_Doc SHALL mendefinisikan unique constraint pada (`topic_id`, `code`).
4. THE Database_Schema_Doc SHALL menyimpan `description` Misconception per Locale pada tabel `misconception_translations` sesuai pola Requirement 3.
5. THE Database_Schema_Doc SHALL mendefinisikan `code` sebagai teks snake_case atau UPPER_SNAKE_CASE yang tidak berubah setelah dibuat.
6. WHEN Admin menonaktifkan sebuah Misconception, THE API_Server SHALL mengubah `is_active` menjadi false tanpa menghapus baris dan tanpa mengubah catatan `user_misconceptions` yang merujuknya.
7. THE Database_Schema_Doc SHALL mendefinisikan tabel `user_misconceptions` dengan kolom `user_id`, `misconception_id`, `source_evaluation_id` (evaluasi jawaban yang mendeteksi), `status` bernilai `detected` atau `resolved`, `detected_at`, dan `resolved_at`, serta unique constraint pada (`user_id`, `misconception_id`).
8. THE Spec_Set SHALL memakai definisi "critical" hanya sebagai `severity` bernilai `critical` pada entri Misconception, tanpa kriteria kritis lain.

### Requirement 7: Autentikasi invite-only (Keputusan D-3)

**User Story:** As an Admin, I want registrasi hanya melalui undangan, so that akses ke Backend_Lab terkontrol.

#### Acceptance Criteria

1. THE Database_Schema_Doc SHALL mendefinisikan tabel `users` dengan kolom `email` unik tidak sensitif huruf besar-kecil, `password_hash`, `role` bernilai `user` atau `admin`, `preferred_locale` nullable, `is_active`, dan `last_login_at`.
2. THE Database_Schema_Doc SHALL mendefinisikan tabel `invitations` dengan kolom `code_hash`, `created_by`, `email` nullable, `role` bernilai `user` atau `admin`, `expires_at`, `used_at` nullable, `used_by` nullable, dan `revoked_at` nullable.
3. THE Database_Schema_Doc SHALL mendefinisikan tabel `refresh_tokens` dengan kolom `user_id`, `family_id`, `token_hash`, `expires_at`, `revoked_at` nullable, `replaced_by` nullable, dan `created_at`.
4. THE Auth_Service SHALL menyimpan `password_hash` dengan algoritma hashing berbasis salt dan memory-hard, dengan nama algoritma dan parameter yang didokumentasikan.
5. THE Auth_Service SHALL menyimpan `code_hash` dan `token_hash` sebagai hash, tanpa menyimpan nilai asli Invite_Code maupun Refresh_Token.
6. WHEN Admin membuat undangan, THE Auth_Service SHALL mengembalikan nilai Invite_Code asli satu kali pada response pembuatan.
7. WHEN request register berisi Invite_Code valid, email unik, dan password yang memenuhi kebijakan, THE Auth_Service SHALL membuat user, menandai undangan `used_at` dan `used_by` dalam satu transaksi, dan menerbitkan token.
8. IF Invite_Code tidak ditemukan, sudah terpakai, dicabut, atau melewati `expires_at`, THEN THE Auth_Service SHALL menolak register dengan HTTP 403 dan `code` `invalid_invite_code`.
9. IF `email` pada undangan terisi dan berbeda dari email register, THEN THE Auth_Service SHALL menolak register dengan HTTP 403 dan `code` `invalid_invite_code`.
10. IF dua request register memakai Invite_Code yang sama secara bersamaan, THEN THE Auth_Service SHALL menerima paling banyak satu request.
11. IF kredensial login salah, THEN THE Auth_Service SHALL mengembalikan HTTP 401 dengan `code` `invalid_credentials` dengan pesan yang sama untuk email tidak terdaftar dan password salah.

### Requirement 8: Token JWT, rotation, dan revocation (Keputusan D-14)

**User Story:** As a Learner, I want sesi login aman dan berkelanjutan, so that saya tidak perlu login ulang namun token yang bocor dapat dibatalkan.

#### Acceptance Criteria

1. THE Auth_Service SHALL menerbitkan Access_Token berformat JWT yang memuat klaim `sub`, `role`, `iat`, `exp`, dan `jti`.
2. THE Auth_Service SHALL menetapkan masa berlaku Access_Token dan Refresh_Token (TTL) dari App_Settings dengan Initial_Value yang tercatat pada ASUMSI A-10, dan perubahan TTL berlaku hanya untuk token yang diterbitkan setelah perubahan (lihat KEPUTUSAN D-10).
3. WHEN request `refresh` berisi Refresh_Token aktif, THE Auth_Service SHALL menerbitkan Access_Token baru dan Refresh_Token baru dalam Token_Family yang sama, lalu mengisi `revoked_at` dan `replaced_by` pada token lama dalam satu transaksi.
4. IF Refresh_Token yang sudah memiliki `revoked_at` dipakai kembali pada `refresh`, THEN THE Auth_Service SHALL me-revoke seluruh Refresh_Token dalam Token_Family yang sama dan mengembalikan HTTP 401 dengan `code` `refresh_token_reuse_detected`.
5. IF Refresh_Token melewati `expires_at` atau tidak ditemukan, THEN THE Auth_Service SHALL mengembalikan HTTP 401 dengan `code` `invalid_refresh_token`.
6. WHEN request `logout` diterima, THE Auth_Service SHALL me-revoke Refresh_Token yang dikirim beserta seluruh Token_Family-nya.
7. WHEN seorang user dinonaktifkan atau password diganti, THE Auth_Service SHALL me-revoke seluruh Refresh_Token milik user tersebut.
8. IF Access_Token kedaluwarsa atau tanda tangannya tidak valid, THEN THE API_Server SHALL mengembalikan HTTP 401 dengan `code` `unauthenticated`.
9. IF role pada Access_Token tidak memenuhi syarat endpoint, THEN THE API_Server SHALL mengembalikan HTTP 403 dengan `code` `forbidden`.
10. THE Auth_Service SHALL membatasi percobaan login dan register melalui Rate_Limiter berbasis token bucket per alamat IP dan per email.
11. WHEN request `refresh` berisi Refresh_Token yang sudah memiliki `revoked_at` karena rotation sah, THE Auth_Service SHALL memperlakukan request tersebut sesuai kriteria 4 tanpa grace window di sisi server.
12. THE API_Contract_Doc SHALL mendokumentasikan kewajiban klien: klien SHALL mengirim paling banyak satu request `refresh` yang sedang berjalan per Refresh_Token pada seluruh tab dan seluruh permintaan paralel (single-flight), dan permintaan lain yang membutuhkan Access_Token baru SHALL menunggu hasil request tersebut dan memakai pasangan token yang dikembalikan.

### Requirement 9: Konvensi API umum

**User Story:** As a frontend developer, I want konvensi API yang seragam, so that integrasi klien dapat diprediksi.

#### Acceptance Criteria

1. THE API_Server SHALL menempatkan seluruh endpoint di bawah prefix `/api/v1`.
2. THE API_Server SHALL mengembalikan setiap error dalam Error_Envelope dengan field `code`, `message`, `details`, dan `request_id`.
3. THE API_Contract_Doc SHALL mendefinisikan daftar `code` error beserta HTTP status, minimal: `validation_error` (400), `unauthenticated` (401), `forbidden` (403), `not_found` (404), `translation_not_available` (404), `conflict` (409), `rate_limited` (429), `ai_quota_exceeded` (429), `ai_provider_unavailable` (503), dan `internal_error` (500).
4. THE API_Server SHALL menetapkan `request_id` pada setiap response melalui header `X-Request-ID`, dan memakai nilai header tersebut dari request bila valid.
5. THE API_Contract_Doc SHALL mendefinisikan cursor pagination (`limit`, `cursor`, `next_cursor`) untuk daftar yang bertambah tanpa batas, termasuk Interview_Session, Transcript, dan assessment attempt.
6. THE API_Contract_Doc SHALL mendefinisikan offset pagination (`limit`, `offset`, `total`) untuk daftar admin berukuran kecil, termasuk Misconception, invitations, dan Golden_Set.
7. THE API_Contract_Doc SHALL menetapkan nilai default dan maksimum `limit` per jenis daftar.
8. IF body request gagal validasi, THEN THE API_Server SHALL mengembalikan HTTP 400 dengan `details` berisi daftar objek dengan field `field` dan `reason`.
9. WHEN request melebihi batas Rate_Limiter, THE API_Server SHALL mengembalikan HTTP 429 dengan `code` `rate_limited` dan header `Retry-After` dalam detik.
10. THE API_Contract_Doc SHALL mendefinisikan, untuk setiap endpoint, method, path, role yang diizinkan, skema request, skema response sukses, dan daftar `code` error yang mungkin.

### Requirement 10: Endpoint API P0

**User Story:** As a frontend developer, I want daftar endpoint P0 yang lengkap, so that seluruh alur Learner dan Admin dapat dibangun.

#### Acceptance Criteria

1. THE API_Contract_Doc SHALL mendefinisikan endpoint autentikasi: `POST /api/v1/auth/register`, `POST /api/v1/auth/login`, `POST /api/v1/auth/refresh`, `POST /api/v1/auth/logout`, dan `GET /api/v1/me`.
2. THE API_Contract_Doc SHALL mendefinisikan endpoint konten Learner: daftar dan detail Topic, serta daftar dan detail Material, dengan parameter `locale`.
3. THE API_Contract_Doc SHALL mendefinisikan endpoint Interview_Session: membuat sesi, memulai, mengirim jawaban Learner, pause, resume, mengakhiri sesi, membatalkan sesi, mengambil Transcript, mengambil hasil evaluasi, dan mendaftar sesi milik Learner.
4. THE API_Contract_Doc SHALL mendefinisikan endpoint progress Learner: MasteryRecord per Topic beserta evidence dan daftar Misconception berstatus `detected`.
5. THE API_Contract_Doc SHALL mendefinisikan endpoint Admin untuk invitations: membuat, mendaftar, dan mencabut undangan yang belum terpakai.
6. THE API_Contract_Doc SHALL mendefinisikan endpoint Admin untuk konten: CRUD entitas induk, CRUD Translation, dan transisi status Translation, untuk Topic, Material, Question, Concept, dan Roadmap.
7. THE API_Contract_Doc SHALL mendefinisikan endpoint Admin untuk Misconception_Catalog: membuat, mengubah `severity` dan Translation, serta menonaktifkan entri.
8. THE API_Contract_Doc SHALL mendefinisikan endpoint Admin untuk Golden_Set: CRUD kasus, menjalankan Golden_Set_Runner, dan membaca hasil run.
9. THE API_Contract_Doc SHALL mendefinisikan endpoint Admin untuk konfigurasi kuota AI, konfigurasi provider, dan pengelolaan serta penggantian versi aktif Prompt_Contract.
10. WHEN endpoint Admin diakses oleh user ber-role selain `admin`, THE API_Server SHALL mengembalikan HTTP 403 dengan `code` `forbidden`.
11. THE API_Contract_Doc SHALL mendefinisikan endpoint health check tanpa autentikasi yang tidak mengungkap konfigurasi internal.
12. THE API_Contract_Doc SHALL mendefinisikan endpoint assessment dan endpoint status AITask (lihat Requirement 16 dan 20).
13. THE API_Contract_Doc SHALL mendefinisikan endpoint rekomendasi dan Roadmap Learner (lihat Requirement 22).
14. THE API_Contract_Doc SHALL mendefinisikan endpoint Evaluation_Retry untuk jawaban assessment dan jawaban interview (lihat Requirement 25).
15. THE API_Contract_Doc SHALL mendefinisikan endpoint Admin untuk membaca dan mengubah App_Settings (lihat Requirement 24).

### Requirement 11: Interview_Session, lifecycle pause/resume, Interview_Message, dan retention

**User Story:** As a Learner, I want sesi interview yang dapat dijeda, dilanjutkan, dan diulang, so that saya dapat berlatih sesuai waktu saya dan memperbaiki penguasaan Topic.

#### Acceptance Criteria

1. THE Database_Schema_Doc SHALL mendefinisikan tabel `interview_sessions` dengan kolom minimum `id`, `user_id`, `status`, `interview_type`, `configuration` (JSON), `current_question_id`, `current_question_index`, `session_state` (JSON), `started_at`, `paused_at`, `expires_at`, `completed_at`, `created_at`, dan `updated_at`, ditambah kolom `locale`, `retry_of_session_id` nullable, `prompt_contract_versions` (JSON), `abandoned_at` nullable, `deleted_at` nullable, dan `retention_until` nullable.
2. THE Database_Schema_Doc SHALL membatasi `status` pada nilai `CREATED`, `IN_PROGRESS`, `PAUSED`, `COMPLETED`, `ABANDONED`, dan `interview_type` pada nilai `practice` dan `recruitment`, melalui constraint `CHECK`.
3. THE API_Contract_Doc SHALL mendefinisikan transisi status Interview_Session yang valid: `CREATED` ke `IN_PROGRESS`, `IN_PROGRESS` ke `PAUSED`, `PAUSED` ke `IN_PROGRESS`, `IN_PROGRESS` ke `COMPLETED`, serta `CREATED`, `IN_PROGRESS`, atau `PAUSED` ke `ABANDONED`.
4. IF sebuah transisi status di luar daftar valid diminta, THEN THE API_Server SHALL menolak request dengan HTTP 409 dan `code` `invalid_session_transition`.
5. THE Database_Schema_Doc SHALL mendefinisikan tabel `interview_messages` dengan kolom `interview_session_id`, `sequence`, `role` bernilai `ai` atau `user`, `kind` bernilai `question`, `follow_up`, atau `answer`, `question_id`, `parent_message_id`, `depth`, `content`, dan `created_at`, serta unique constraint pada (`interview_session_id`, `sequence`). Himpunan `interview_messages` milik satu Interview_Session adalah Transcript.
6. WHEN Learner memanggil pause pada Interview_Session `IN_PROGRESS`, THE API_Server SHALL mengubah `status` menjadi `PAUSED`, mengisi `paused_at` dengan waktu sekarang, mengisi `expires_at` sebesar `paused_at` ditambah 7 hari, dan mempertahankan `session_state`, `current_question_id`, dan `current_question_index`.
7. WHEN Learner memanggil resume pada Interview_Session `PAUSED` yang `expires_at`-nya belum terlewati, THE API_Server SHALL mengubah `status` menjadi `IN_PROGRESS`, mengosongkan `paused_at` dan `expires_at`, dan mengembalikan pesan AI terakhir yang belum dijawab beserta `session_state` ringkas.
8. WHEN `expires_at` sebuah Interview_Session `PAUSED` terlewati, THE Backend_Lab SHALL mengubah `status` menjadi `ABANDONED` dan mengisi `abandoned_at` melalui job terjadwal, dan THE API_Server SHALL memperlakukan sesi tersebut sebagai `ABANDONED` pada resume yang terjadi sebelum job berjalan dengan HTTP 409 dan `code` `session_expired`.
9. WHEN Learner membuat sesi baru pada Topic yang sama, THE API_Server SHALL membuat Interview_Session baru tanpa pembatasan jumlah dan tanpa jeda waktu, serta mengisi `retry_of_session_id` bila Learner menyebut sesi sebelumnya (lihat ASUMSI A-4).
10. THE API_Server SHALL mengizinkan Learner mengakses hanya Interview_Session milik Learner tersebut.
11. IF Learner mengirim jawaban ke Interview_Session yang berstatus selain `IN_PROGRESS`, THEN THE API_Server SHALL mengembalikan HTTP 409 dengan `code` `session_not_active`.
12. WHERE `retention_until` terisi pada sebuah Interview_Session, THE Database_Schema_Doc SHALL mendefinisikan job pembersihan yang mengisi `deleted_at` setelah `retention_until` terlewati.
13. WHILE `retention_until` bernilai null, THE Backend_Lab SHALL menyimpan Transcript tanpa batas waktu (lihat ASUMSI A-6).
14. WHEN `deleted_at` sebuah Interview_Session terisi, THE API_Server SHALL mengecualikan sesi dan Transcript tersebut dari seluruh response Learner.
15. THE Database_Schema_Doc SHALL mendefinisikan tabel `interview_evaluations` dengan kolom `interview_session_id` unik, `status`, `dimension_scores` (JSON), `weights` (JSON), `overall_score`, `summary`, `prompt_contract_id`, `model_name`, `raw_output` (JSON), dan `created_at`.

### Requirement 12: MasteryRecord, Attempt, dan progress Learner

**User Story:** As a Learner, I want status penguasaan per Topic beserta buktinya, so that saya tahu Topic mana yang sudah dikuasai dan alasannya.

#### Acceptance Criteria

1. THE Database_Schema_Doc SHALL mendefinisikan tabel `topic_attempts` sebagai ledger Attempt dengan kolom `user_id`, `topic_id`, `source_type` bernilai `assessment` atau `interview`, `assessment_attempt_id` nullable, `interview_session_id` nullable, `score_pct` pada rentang 0 sampai 100, `has_application_question`, dan `scored_at`.
2. THE Database_Schema_Doc SHALL mendefinisikan tabel `mastery_records` dengan kolom `user_id`, `topic_id`, `status`, `attempt_count`, `avg_last3` nullable, `best_score` nullable, `last_attempt_id` nullable, `mastered_at` nullable, `interview_ready` boolean nullable, `evidence` (JSON), dan `rule_version`, serta unique constraint pada (`user_id`, `topic_id`).
3. THE Database_Schema_Doc SHALL membatasi `status` pada nilai `NOT_STARTED`, `IN_PROGRESS`, `PRACTICING`, `NEEDS_REVIEW`, `MASTERED` melalui constraint `CHECK`.
4. THE Mastery_Engine SHALL menyimpan pada `evidence` setiap MasteryRecord: daftar Attempt ID yang dipakai, jendela score, flag Misconception (kode katalog, severity, status), flag `ever_mastered` (boolean yang menjadi true setelah status `MASTERED` pernah tercapai dan tidak kembali ke false), dan daftar alasan yang menahan status `MASTERED`.
5. THE Backend_Lab SHALL menyimpan Interview_Ready_Flag tanpa rumus perhitungan, dan nilai awalnya null (lihat ASUMSI A-5).
6. WHEN sebuah Attempt selesai dinilai, THE Mastery_Engine SHALL menyimpan `topic_attempts`, memperbarui `user_misconceptions`, dan memperbarui `mastery_records` dalam satu transaksi database.
7. THE API_Contract_Doc SHALL mengekspos `interview_ready` sebagai nilai nullable dan `evidence` pada response progress.

### Requirement 13: Mastery v1 dan siklus Misconception (Keputusan D-1, D-5, D-6, D-12)

**User Story:** As a product owner, I want keputusan MASTERED yang deterministik, berbasis bukti, dan memperhitungkan misconception kritis, so that status penguasaan dapat dipercaya.

#### Acceptance Criteria

1. THE Evaluation_Algorithm_Doc SHALL mendefinisikan algoritma Mastery v1 yang menghasilkan `status` dan `evidence` secara deterministik untuk himpunan Attempt dan `user_misconceptions` yang sama.
2. WHEN seorang Learner pada sebuah Topic memiliki sedikitnya 3 Attempt, rata-rata `score_pct` dari 3 Attempt terakhir mencapai atau melebihi `mastered_threshold` (default 80), tidak ada Unresolved_Critical_Misconception, dan sedikitnya satu Attempt dalam jendela tersebut memiliki `has_application_question` bernilai true, THE Mastery_Engine SHALL menetapkan status `MASTERED`.
3. IF salah satu syarat pada kriteria 2 tidak terpenuhi, THEN THE Mastery_Engine SHALL menetapkan status selain `MASTERED` dan mencatat setiap syarat yang tidak terpenuhi pada `evidence`.
4. THE Evaluation_Algorithm_Doc SHALL mendefinisikan penetapan `NOT_STARTED`, `IN_PROGRESS`, `PRACTICING`, dan `NEEDS_REVIEW` sebagai fungsi deterministik dari jumlah Attempt, rata-rata 3 Attempt terakhir, Unresolved_Critical_Misconception, dan flag `ever_mastered`, dengan aturan sementara yang disetujui pada KEPUTUSAN D-6: `NOT_STARTED` tanpa Attempt, `IN_PROGRESS` dengan 1 sampai 2 Attempt, `NEEDS_REVIEW` bila ada Unresolved_Critical_Misconception, atau `ever_mastered` bernilai true, atau rata-rata 3 Attempt terakhir berada di bawah `needs_review_floor` (Initial_Value 50) pada sedikitnya 3 Attempt, dan `PRACTICING` untuk kondisi lain pada sedikitnya 3 Attempt. Urutan evaluasi adalah `NOT_STARTED`, `MASTERED`, `NEEDS_REVIEW`, `PRACTICING`, lalu `IN_PROGRESS`, sehingga `NEEDS_REVIEW` diutamakan atas `IN_PROGRESS` bila ada Unresolved_Critical_Misconception pada 1 sampai 2 Attempt.
5. WHILE Learner memiliki setidaknya satu Unresolved_Critical_Misconception pada sebuah Topic, THE Mastery_Engine SHALL mempertahankan status selain `MASTERED` untuk Topic tersebut, berapa pun `score_pct`.
6. WHEN Evaluator melaporkan sebuah Misconception terdeteksi, THE Mastery_Engine SHALL membuat, membuka kembali, atau mempertahankan catatan `user_misconceptions` berstatus `detected` untuk Misconception tersebut.
7. WHEN evaluasi pada Attempt berikutnya melaporkan bukti penguasaan atas Misconception berstatus `detected` sebanyak `resolve_consecutive_required` kali berturut-turut (default 1), THE Mastery_Engine SHALL mengubah status catatan menjadi `resolved` dan mengisi `resolved_at` (lihat ASUMSI A-13).
8. WHEN Admin menandai sebuah catatan `user_misconceptions` sebagai `resolved` secara manual, THE API_Server SHALL mengubah status dan mencatat `resolved_by` serta alasan, lalu THE Mastery_Engine SHALL mengevaluasi ulang MasteryRecord Topic tersebut.
9. WHEN Admin menonaktifkan sebuah Critical_Misconception, THE Mastery_Engine SHALL tetap memperlakukan catatan `detected` yang merujuknya sebagai Unresolved_Critical_Misconception sampai catatan tersebut berstatus `resolved`.
10. THE Evaluation_Algorithm_Doc SHALL menyertakan tabel keputusan dan contoh numerik untuk kombinasi (jumlah Attempt, rata-rata 3 terakhir, Unresolved_Critical_Misconception, Attempt aplikatif) yang mencakup setiap status.
11. THE Evaluation_Algorithm_Doc SHALL menetapkan bahwa Misconception berstatus `minor` menurunkan `score_pct` jawaban sesuai bobot konfigurasi tanpa memblokir `MASTERED`.
12. IF sebuah evaluasi jawaban berstatus `failed` atau `unscored`, THEN THE Mastery_Engine SHALL mempertahankan `mastery_records` dan `user_misconceptions` tanpa perubahan akibat jawaban tersebut.
13. THE Evaluation_Algorithm_Doc SHALL menandai aturan pada kriteria 4 sebagai aturan sementara yang disetujui (versi `rule_version` Mastery v1), dan nilai ambangnya SHALL dibaca dari App_Settings.

### Requirement 14: Kontrak output Evaluator berbasis katalog (Keputusan D-1)

**User Story:** As an Admin, I want Evaluator hanya memilih dari katalog dan konsep yang diberikan, so that tidak ada misconception atau konsep liar yang masuk ke data Learner.

#### Acceptance Criteria

1. THE AI_Prompt_Contract_Doc SHALL mendefinisikan input Task_Type `answer_evaluation` yang memuat pertanyaan, daftar Concept pertanyaan (`code`, `description`, `weight`, `is_required`), jawaban Learner sebagai data tidak tepercaya, Locale, daftar Misconception aktif milik Topic (`code`, `description`; tanpa `severity`), dan daftar `code` Misconception berstatus `detected` milik Learner.
2. THE AI_Prompt_Contract_Doc SHALL mendefinisikan JSON Schema output `answer_evaluation` dengan field `concept_results`, `misconception_codes`, `resolved_misconception_codes`, `relevance`, `score_hint`, `feedback`, `confidence`, dan `injection_suspected`.
3. WHEN output Evaluator diterima, THE AI_Gateway SHALL memvalidasi output terhadap JSON Schema output.
4. WHEN output Evaluator diterima, THE AI_Gateway SHALL memverifikasi bahwa setiap `code` pada `misconception_codes` ada pada Misconception_Catalog aktif Topic pertanyaan, setiap `code` pada `resolved_misconception_codes` ada pada daftar `detected` milik Learner, kedua daftar tidak beririsan, dan `concept_results` memuat tepat satu entri untuk setiap Concept input.
5. IF output Evaluator memuat `code` Misconception atau Concept di luar input, THEN THE AI_Gateway SHALL menolak output tersebut tanpa menyimpan `code` itu ke `user_misconceptions` dan mencatat percobaan sebagai gagal validasi.
6. IF output Evaluator gagal validasi, THEN THE AI_Gateway SHALL mengulang pemanggilan hingga `max_validation_retries` kali (parameter App_Settings, Initial_Value 2), dan setelah itu menetapkan AITask `failed` serta evaluasi jawaban berstatus `failed` yang dapat di-Evaluation_Retry (lihat Requirement 25).
7. THE AI_Prompt_Contract_Doc SHALL menyertakan instruksi pada prompt yang membatasi Evaluator memilih `code` hanya dari daftar input, dan field `severity` serta skor final tidak boleh ditentukan oleh output Evaluator.
8. THE Mastery_Engine SHALL menentukan `severity` setiap Misconception terdeteksi dari `misconceptions.severity` pada database, bukan dari output Evaluator.
9. THE API_Server SHALL menghitung `score_pct` final jawaban di server dari `concept_results` dan Misconception, dan THE API_Server SHALL memakai `score_hint` hanya sebagai sinyal konsistensi.

### Requirement 15: Prompt contracts umum, versioning, dan keamanan

**User Story:** As an AI engineer, I want kontrak prompt yang berversi dan aman, so that perilaku AI dapat direproduksi dan diaudit.

#### Acceptance Criteria

1. THE AI_Prompt_Contract_Doc SHALL mendefinisikan registry Task_Type yang memuat minimal `answer_evaluation`, `interview_followup`, dan `interview_evaluation`.
2. THE AI_Prompt_Contract_Doc SHALL mendefinisikan setiap Prompt_Contract dengan field `contract_id`, `task_type`, `version`, `locale_policy`, `input_schema`, `output_schema`, `system_prompt`, `parameters` (termasuk `temperature`, `max_output_tokens`, `timeout_seconds`), dan `status`.
3. THE Database_Schema_Doc SHALL mendefinisikan tabel `prompt_contracts` dengan unique constraint pada (`task_type`, `version`) dan partial unique index yang menjamin paling banyak satu baris `status` `active` per `task_type`.
4. THE Database_Schema_Doc SHALL mendefinisikan `task_type` sebagai referensi ke tabel registry, sehingga Task_Type baru (termasuk AI tutoring pada materials) dapat ditambahkan tanpa perubahan struktur tabel.
5. THE AI_Prompt_Contract_Doc SHALL menyatakan AI tutoring dalam materials berada di luar cakupan P0 dan hanya tersedia sebagai ekstensi melalui penambahan Task_Type (lihat ASUMSI A-7).
6. WHEN sebuah Interview_Session dibuat, THE AI_Gateway SHALL menyimpan versi Prompt_Contract aktif untuk setiap Task_Type interview pada `interview_sessions.prompt_contract_versions`, dan seluruh AITask sesi tersebut SHALL memakai versi yang disimpan.
7. THE AI_Prompt_Contract_Doc SHALL menetapkan bahwa jawaban Learner dan Transcript dimasukkan ke prompt sebagai data pada bagian terpisah yang diberi delimiter, dan bahwa instruksi di dalam data tersebut tidak mengubah perilaku Evaluator (lihat Requirement 23).
8. THE AI_Prompt_Contract_Doc SHALL memuat contoh input dan output valid serta contoh output tidak valid untuk setiap Task_Type.
9. THE AI_Prompt_Contract_Doc SHALL mendefinisikan kontrak Task_Type `interview_followup` dengan output JSON yang memuat `follow_up_question`, `target_concept_codes`, dan `difficulty`, dalam Locale sesi.
10. WHEN versi aktif sebuah Prompt_Contract untuk `answer_evaluation` hendak diganti, THE API_Server SHALL mensyaratkan hasil Golden_Set_Runner terbaru untuk versi calon yang memenuhi ambang lulus (lihat Requirement 17) dan berasal dari run Golden_Run_Mode `live`, kecuali pengaktifan membawa alasan waiver tercatat.
11. THE Database_Schema_Doc SHALL menyimpan perubahan Prompt_Contract sebagai versi baru yang immutable setelah keluar dari status `draft`.

### Requirement 16: AI_Gateway, AITask, kuota, rate limit, dan antrean (Keputusan D-10, D-15)

**User Story:** As an operator, I want penggunaan AI terukur, adil antar user, dan tahan terhadap rate limit provider, so that biaya dan ketersediaan terkendali tanpa ketergantungan pada Redis.

#### Acceptance Criteria

1. THE Database_Schema_Doc SHALL mendefinisikan tabel `ai_usage_logs` dengan kolom `user_id`, `ai_task_id`, `task_type`, `prompt_contract_version`, `provider_name`, `model_name`, `input_tokens`, `output_tokens`, `latency_ms`, `status`, `request_id`, dan `created_at`.
2. THE Database_Schema_Doc SHALL menyimpan kuota AI pada App_Settings dengan dimensi `scope` (`global` atau `user`), `period` (`daily` atau `monthly`), dan `metric`, beserta Initial_Value yang dapat diubah Admin tanpa deploy ulang (lihat ASUMSI A-14 dan KEPUTUSAN D-10).
3. WHEN sebuah AITask hendak dibuat, THE Quota_Manager SHALL memeriksa pemakaian periode berjalan terhadap `limit_value` sebelum menerima task.
4. IF pemakaian mencapai `limit_value`, THEN THE API_Server SHALL mengembalikan HTTP 429 dengan `code` `ai_quota_exceeded` dan `details.reset_at` berisi waktu reset periode dalam UTC.
5. THE Database_Schema_Doc SHALL mendefinisikan tabel `ai_tasks` dengan kolom `user_id`, `task_type`, `priority`, `status` bernilai `queued`, `processing`, `completed`, atau `failed`, `payload` (JSON), `result` (JSON), `attempts`, `max_attempts`, `run_after`, `locked_at`, `locked_by`, `error_code`, dan `provider_name`.
6. THE Rate_Limiter SHALL mengimplementasikan token bucket untuk request HTTP per kelas endpoint (auth, AI, umum) dengan parameter `capacity` dan `refill_rate`.
7. THE AI_Gateway SHALL mengimplementasikan token bucket RPM dan TPM per provider sebelum setiap pemanggilan model, dan menjadwalkan ulang AITask dengan `run_after` bila token tidak cukup.
8. WHEN provider mengembalikan HTTP 429, THE AI_Gateway SHALL mencoba ulang dengan exponential backoff disertai jitter, dan menghormati header `Retry-After` bila ada.
9. IF percobaan pada sebuah provider habis atau provider mengembalikan error server atau timeout, THEN THE AI_Gateway SHALL mencoba provider fallback berikutnya menurut urutan konfigurasi.
10. IF seluruh provider gagal hingga `max_attempts`, THEN THE AI_Gateway SHALL menetapkan AITask `failed` dengan `error_code` `ai_provider_unavailable`.
11. THE Quota_Manager SHALL membatasi jumlah AITask berstatus `queued` atau `processing` per user (`per_user_queue_limit`, parameter App_Settings dengan Initial_Value), dan WHEN batas tercapai THE API_Server SHALL mengembalikan HTTP 429 dengan `code` `ai_queue_limit_exceeded`.
12. THE Job_Queue SHALL memilih AITask berdasarkan prioritas, dengan urutan dari tertinggi: task interview aktif (jawaban interview dan `interview_followup`), evaluasi jawaban assessment dan evaluasi akhir interview, lalu background generation.
13. WHEN worker mengambil AITask, THE Job_Queue SHALL memastikan satu AITask diambil paling banyak oleh satu worker pada satu waktu, dan THE Job_Queue SHALL mengembalikan AITask `processing` yang melewati `lock_timeout` ke status `queued`.
14. THE AI_Gateway SHALL mencatat satu baris `ai_usage_logs` untuk setiap percobaan pemanggilan model, termasuk yang gagal.
15. WHERE Redis tersedia, THE Backend_Lab SHALL mengizinkan penggantian penyimpanan Rate_Limiter dan Job_Queue melalui antarmuka adapter tanpa perubahan kontrak API.
16. THE API_Contract_Doc SHALL mendefinisikan endpoint status AITask yang hanya dapat dibaca oleh pemilik task dan Admin.
17. WHERE deployment berjalan dengan lebih dari satu instance API_Server atau worker, THE Backend_Lab SHALL memakai PostgreSQL sebagai penyimpanan bersama untuk Job_Queue dan state token bucket Rate_Limiter (termasuk token bucket RPM dan TPM per provider), dan THE Database_Schema_Doc SHALL mendefinisikan tabel penyimpanan state token bucket tersebut (lihat KEPUTUSAN D-15).
18. WHILE deployment berjalan dengan satu instance atau pada lingkungan dev, THE Backend_Lab SHALL mengizinkan mode in-process untuk Job_Queue dan Rate_Limiter, dan THE Backend_Lab SHALL mengaktifkan penyimpanan PostgreSQL sebagai nilai default konfigurasi deployment multi-instance.

### Requirement 17: Golden_Set dan regresi Evaluator (Keputusan D-13)

**User Story:** As an Admin, I want golden set untuk menguji Evaluator, so that perubahan prompt atau model tidak menurunkan kualitas penilaian.

#### Acceptance Criteria

1. THE Database_Schema_Doc SHALL mendefinisikan tabel `golden_cases` dengan kolom `question_id`, `locale`, `category`, `sample_answer`, `expected_score_min`, `expected_score_max`, `expected_found_concept_codes`, `expected_missed_concept_codes`, `expected_misconception_codes` (kode katalog), `owner_id`, dan `is_active`.
2. THE Evaluation_Algorithm_Doc SHALL mendefinisikan kategori Golden_Set: `different_wording`, `partially_correct`, `critical_misconception`, `off_topic_or_empty`, dan `adversarial_injection`.
3. THE Database_Schema_Doc SHALL mendefinisikan tabel `golden_runs` dan `golden_run_results` yang menyimpan `prompt_contract_id`, `model_name`, `run_mode` (`recorded` atau `live`), hasil per kasus, dan metrik agregat.
4. WHEN Admin menyimpan `golden_cases`, THE API_Server SHALL memvalidasi bahwa `expected_score_min` tidak melebihi `expected_score_max`, setiap kode Misconception ada pada Misconception_Catalog Topic pertanyaan, setiap kode Concept ada pada Concept pertanyaan, dan `locale` memiliki Published_Translation pertanyaan.
5. THE Evaluation_Algorithm_Doc SHALL mendefinisikan `min_cases_per_category` sebagai parameter konfigurasi, dengan nilai default tercatat sebagai ASUMSI A-15.
6. IF jumlah kasus aktif pada sebuah kategori kurang dari `min_cases_per_category`, THEN THE Golden_Set_Runner SHALL menandai run sebagai `insufficient_coverage` dan tidak menyatakan run tersebut lulus.
7. WHEN Golden_Set_Runner dijalankan, THE Golden_Set_Runner SHALL menghitung per run: `score_in_range_rate`, `concept_f1`, `critical_recall`, `false_critical_rate`, `injection_resilience_rate`, `catalog_violation_count`, dan `schema_violation_count`.
8. THE Evaluation_Algorithm_Doc SHALL mendefinisikan ambang lulus untuk setiap metrik sebagai parameter App_Settings dengan Initial_Value, dengan syarat `catalog_violation_count` dan `schema_violation_count` sama dengan 0 untuk run lulus.
9. THE Evaluation_Algorithm_Doc SHALL mendefinisikan perbandingan regresi terhadap run baseline dari versi aktif, beserta toleransi penurunan metrik.
10. THE Evaluation_Algorithm_Doc SHALL menyertakan rumus setiap metrik dan contoh perhitungan numerik.
11. THE Golden_Set SHALL dimiliki oleh Admin, dan THE API_Server SHALL mengizinkan perubahan Golden_Set hanya oleh Admin.
12. WHERE Golden_Run_Mode bernilai `recorded`, THE Golden_Set_Runner SHALL menilai response model yang direkam untuk setiap kasus tanpa memanggil AI provider, dan THE Backend_Lab SHALL memakai mode ini pada pipeline CI.
13. WHERE Golden_Run_Mode bernilai `live`, THE Golden_Set_Runner SHALL memanggil model nyata melalui AI_Gateway, dan THE Backend_Lab SHALL memicu run ini secara manual oleh Admin atau melalui jadwal berkala di luar pipeline CI.
14. WHEN sebuah run `live` selesai, THE Golden_Set_Runner SHALL menyimpan response model sebagai rekaman yang dapat dipakai run `recorded` berikutnya untuk pasangan (`prompt_contract_id`, `model_name`) (lihat ASUMSI A-26).
15. THE Golden_Set_Runner SHALL menyimpan `run_mode` pada setiap `golden_runs` dan THE Evaluation_Algorithm_Doc SHALL menetapkan bahwa hanya run `live` yang dihitung sebagai baseline regresi dan sebagai syarat aktivasi Prompt_Contract.

### Requirement 18: Keamanan, privasi, dan observabilitas

**User Story:** As a security-conscious operator, I want kontrol keamanan dasar terdokumentasi, so that data user dan token terlindungi.

#### Acceptance Criteria

1. THE API_Server SHALL mengecualikan `password_hash`, `token_hash`, dan `code_hash` dari seluruh response dan log.
2. THE API_Contract_Doc SHALL mendefinisikan kebijakan password dengan panjang minimum dan aturan validasi yang dikonfigurasi.
3. THE API_Server SHALL mengizinkan Learner membaca hanya data milik Learner tersebut, kecuali konten yang dipublikasikan.
4. THE API_Server SHALL menulis log terstruktur yang memuat `request_id`, `user_id` bila ada, method, path, status, dan durasi.
5. THE API_Server SHALL menetapkan konfigurasi CORS berbasis daftar origin yang diizinkan.
6. THE Backend_Lab SHALL membaca secret (kunci penandatanganan JWT, kredensial AI provider, kredensial database) dari environment atau secret store.
7. IF sebuah error internal terjadi, THEN THE API_Server SHALL mengembalikan `code` `internal_error` tanpa stack trace dan tanpa detail infrastruktur pada `message` maupun `details`.

### Requirement 19: Cakupan P0, P1, dan P2

**User Story:** As a project planner, I want batas cakupan yang jelas, so that P0 dapat dikirim tanpa menunggu P1 dan P2.

#### Acceptance Criteria

1. THE Spec_Set SHALL menspesifikasikan seluruh fitur P0 dari Product Spec v1.1 secara penuh pada keempat dokumen turunan.
2. THE Database_Schema_Doc SHALL menempatkan skema RAG berbasis `pgvector` (P1) pada migrasi terpisah yang tidak diperlukan oleh fitur P0.
3. THE Spec_Set SHALL mendeskripsikan fitur P2 hanya sebagai titik ekstensi (extension point) dengan satu paragraf per titik, tanpa tabel, endpoint, atau Prompt_Contract rinci.
4. THE Spec_Set SHALL menandai setiap bagian dokumen dengan label prioritas `P0`, `P1`, atau `P2`.

### Requirement 20: Question bank, assessment, dan scoring free-text

**User Story:** As a Learner, I want menjawab pertanyaan free-text dan mendapat skor yang adil dan dapat dijelaskan, so that saya tahu konsep apa yang sudah dan belum saya kuasai.

#### Acceptance Criteria

1. THE Database_Schema_Doc SHALL mendefinisikan tabel `questions` dengan kolom metadata `topic_id`, `cognitive_level` bernilai `recall`, `conceptual`, `application`, atau `scenario`, `difficulty`, `purpose` bernilai `assessment`, `interview`, atau `both`, dan `weight`, serta tabel `question_translations` sesuai pola Requirement 3 dengan status `draft`, `in_review`, `published`, `archived`.
2. THE Database_Schema_Doc SHALL mendefinisikan tabel `concepts` dan `question_concepts` (dengan `weight` dan `is_required`) sehingga setiap pertanyaan memiliki daftar Concept yang dinilai.
3. THE Content_Service SHALL menyajikan pertanyaan kepada Learner hanya WHEN pertanyaan memiliki Published_Translation pada Locale permintaan.
4. THE Translation_Service SHALL menolak transisi Translation pertanyaan ke `published` IF pertanyaan tidak memiliki sedikitnya satu Concept atau `reference_answer` kosong.
5. THE API_Contract_Doc SHALL mendefinisikan endpoint assessment: membuat attempt untuk sebuah Topic, menyimpan jawaban per pertanyaan, men-submit attempt, dan membaca hasil beserta umpan balik per pertanyaan.
6. WHEN sebuah attempt dibuat, THE Assessment_Service SHALL memilih pertanyaan `published` pada Topic dan Locale tersebut dengan `purpose` `assessment` atau `both`, dan SHALL menyertakan sedikitnya satu pertanyaan ber-`cognitive_level` `application` atau `scenario` bila tersedia.
7. WHEN sebuah attempt di-submit, THE Assessment_Service SHALL membuat satu AITask `answer_evaluation` untuk setiap jawaban non-kosong, dan SHALL memberi skor 0 tanpa memanggil AI untuk jawaban kosong.
8. THE Evaluation_Algorithm_Doc SHALL mendefinisikan scoring free-text yang menghitung `score_pct` dari `concept_results` berbobot, penalti Misconception, dan pembatasan (clamp) rentang 0 sampai 100, secara deterministik untuk output Evaluator tervalidasi yang sama.
9. THE Evaluation_Algorithm_Doc SHALL menetapkan skor 0 untuk jawaban dengan `relevance` bernilai `off_topic` atau `empty`.
10. WHEN setiap jawaban pada sebuah attempt memiliki Answer_Evaluation_Status `scored` atau `unscored`, THE Assessment_Service SHALL menghitung `score_pct` attempt sebagai rata-rata berbobot `weight` pertanyaan hanya atas jawaban `scored`, dan membuat satu baris `topic_attempts`.
11. IF seluruh jawaban non-kosong pada sebuah attempt berstatus `unscored`, THEN THE Assessment_Service SHALL menandai attempt sebagai `unscored` tanpa membuat baris `topic_attempts`.
12. WHILE sebuah jawaban pada attempt berstatus `pending` atau `failed`, THE Assessment_Service SHALL menunda pembuatan `topic_attempts` untuk attempt tersebut (lihat Requirement 25).

### Requirement 21: Batas interview, alokasi follow-up, dan scoring interview (Keputusan D-7, D-8, D-9, D-11, D-12)

**User Story:** As a Learner, I want interview yang terkendali panjangnya dan dinilai pada beberapa dimensi, so that sesi berjalan wajar dan hasilnya informatif.

#### Acceptance Criteria

1. THE Database_Schema_Doc SHALL mendefinisikan tabel `interview_profiles` per `interview_type` dengan `min_questions`, `max_questions`, `max_followups_per_question`, `total_cap`, `max_depth`, dan bobot dimensi, dengan nilai seed Practice (5 sampai 10 pertanyaan, maksimum 2 follow-up per pertanyaan, total maksimum 10, depth 2) dan Recruitment (6 sampai 10 pertanyaan, maksimum 3 follow-up per pertanyaan, total maksimum 15, depth 3).
2. WHEN sebuah Interview_Session dibuat, THE API_Server SHALL menyalin nilai profil ke `configuration` sesi sehingga perubahan profil tidak memengaruhi sesi berjalan.
3. THE Evaluation_Algorithm_Doc SHALL menetapkan `total_cap` sebagai batas yang mengikat: jumlah seluruh pertanyaan utama dan follow-up pada sebuah sesi tidak melebihi `total_cap`, walaupun batas per pertanyaan belum tercapai.
4. THE Evaluation_Algorithm_Doc SHALL mendefinisikan alokasi follow-up yang menjamin setiap pertanyaan utama tetap dapat diajukan (follow-up tidak menghabiskan anggaran pertanyaan utama yang tersisa), jumlah follow-up per pertanyaan tidak melebihi `max_followups_per_question`, dan depth rantai follow-up tidak melebihi `max_depth`.
5. THE Evaluation_Algorithm_Doc SHALL menetapkan keputusan follow-up berdasarkan hasil evaluasi jawaban (skor, Concept `required` yang terlewat, Misconception terdeteksi), sedangkan AI hanya menyusun teks follow-up.
6. THE Evaluation_Algorithm_Doc SHALL mendefinisikan skor interview sebagai jumlah berbobot lima dimensi: Technical Accuracy 30, Conceptual Understanding 25, Reasoning 20, Practical Application 15, dan Communication 10, dengan seed bobot 30/25/20/15/10 yang sama untuk `interview_type` `practice` dan `recruitment`, tersimpan per `interview_type` pada `interview_profiles` dan dapat diubah per tipe (lihat KEPUTUSAN D-9).
7. THE API_Server SHALL menghitung `overall_score` interview di server dari `dimension_scores` hasil `interview_evaluation` yang sudah divalidasi dan di-clamp ke rentang 0 sampai 100.
8. WHEN sebuah Interview_Session selesai, THE Mastery_Engine SHALL membuat satu `topic_attempts` per Topic yang tercakup dengan `score_pct` rata-rata berbobot skor jawaban `scored` pada Topic tersebut, dengan pertanyaan utama dan follow-up digabung per pertanyaan utama (lihat KEPUTUSAN D-7). `has_application_question` bernilai true bila sedikitnya satu pertanyaan utama pada Topic tersebut ber-`cognitive_level` `application` atau `scenario` dan dijawab non-kosong.
9. THE Mastery_Engine SHALL menghitung `overall_score` dimensi interview terpisah dari skor Attempt, dan THE Mastery_Engine SHALL tidak memakai `overall_score` sebagai input `topic_attempts.score_pct` maupun Mastery v1 (lihat KEPUTUSAN D-8).
10. WHEN Learner membuat Interview_Session, THE API_Server SHALL menerima daftar Topic sebanyak `min_topics_per_session` sampai `max_topics_per_session` (App_Settings, Initial_Value 1 dan 5), dan IF jumlah Topic di luar rentang tersebut, THEN THE API_Server SHALL menolak request dengan HTTP 400 dan `code` `validation_error` (batas sementara, lihat KEPUTUSAN D-11).
11. IF seluruh jawaban pada sebuah Topic dalam Interview_Session berstatus `unscored`, THEN THE Mastery_Engine SHALL tidak membuat `topic_attempts` untuk Topic tersebut.
12. IF evaluasi sebuah jawaban interview berstatus `failed`, THEN THE Evaluation_Algorithm_Doc SHALL menetapkan bahwa sesi berlanjut ke pertanyaan utama berikutnya tanpa follow-up untuk jawaban tersebut, sampai jawaban itu berstatus `scored` melalui Evaluation_Retry.

### Requirement 22: Rekomendasi rule-based

**User Story:** As a Learner, I want rekomendasi langkah belajar yang jelas alasannya, so that saya tahu apa yang perlu dipelajari berikutnya.

#### Acceptance Criteria

1. THE Evaluation_Algorithm_Doc SHALL mendefinisikan rule engine rekomendasi deterministik dengan prioritas berurutan: (1) prerequisite belum `MASTERED`, (2) mastery rendah, (3) relevan dengan roadmap Learner, (4) urutan kurikulum.
2. THE API_Contract_Doc SHALL mengembalikan setiap rekomendasi dengan field `rule` yang memuat `code`, `priority`, dan `params` penyebab rekomendasi.
3. THE Recommendation_Service SHALL merekomendasikan hanya Topic yang memiliki Published_Translation pada Locale permintaan.
4. IF sebuah Topic memenuhi lebih dari satu rule, THEN THE Recommendation_Service SHALL menampilkan satu rekomendasi untuk Topic tersebut dengan rule berprioritas tertinggi.
5. THE Recommendation_Service SHALL mengembalikan daftar yang sama untuk keadaan MasteryRecord, roadmap, dan konten yang sama.
6. THE Database_Schema_Doc SHALL mendefinisikan `topic_prerequisites`, `roadmaps`, `roadmap_translations`, `roadmap_topics`, dan `user_roadmaps` untuk mendukung rule rekomendasi.

### Requirement 23: Pertahanan prompt injection

**User Story:** As a security-conscious operator, I want pertahanan berlapis terhadap prompt injection, so that jawaban Learner tidak dapat memanipulasi skor atau data lain.

#### Acceptance Criteria

1. THE AI_Gateway SHALL menempatkan isi dari Learner pada pesan user terpisah dengan delimiter berisi nonce acak per pemanggilan, dan tidak pernah menyisipkannya ke system prompt.
2. WHEN isi dari Learner memuat string yang menyerupai delimiter, THE AI_Gateway SHALL menetralkan string tersebut sebelum menyusun prompt.
3. THE AI_Gateway SHALL memvalidasi setiap output AI terhadap JSON Schema dengan `additionalProperties` bernilai false.
4. THE API_Server SHALL melakukan clamp skor di server pada rentang 0 sampai 100 dan menolak nilai di luar rentang pada output AI.
5. WHEN heuristik atau flag `injection_suspected` mendeteksi dugaan injection, THE AI_Gateway SHALL mencatat satu baris pada tabel `ai_injection_events` tanpa menghentikan penilaian, dan tanpa mengubah aturan scoring.
6. THE Evaluation_Algorithm_Doc SHALL menyertakan kategori Golden_Set `adversarial_injection` dengan metrik `injection_resilience_rate`.

### Requirement 24: App_Settings dan nilai awal parameter (Keputusan D-10)

**User Story:** As an Admin, I want parameter scoring dan AI tersimpan sebagai konfigurasi yang jelas statusnya, so that nilai awal dapat disesuaikan tanpa deploy ulang dan tidak disangka sebagai nilai final.

#### Acceptance Criteria

1. THE Database_Schema_Doc SHALL mendefinisikan tabel `app_settings` dengan kolom `key` unik, `value` (JSON), `description`, `is_initial_value` (boolean), `updated_by` nullable, dan `updated_at`.
2. THE Database_Schema_Doc SHALL menyertakan seed `app_settings` dengan `is_initial_value` bernilai true untuk parameter berikut: bobot penalti Misconception `minor` dan batasnya, penalti dan batas skor Critical_Misconception, nilai kredit `partial`, batas clamp skor, ambang metrik Golden_Set, `min_cases_per_category`, kuota AI per `scope`, `period`, dan `metric`, `per_user_queue_limit`, TTL Access_Token, TTL Refresh_Token, `max_validation_retries`, `max_evaluation_retries`, `evaluation_retry_window_hours`, `min_topics_per_session`, `max_topics_per_session`, dan parameter Mastery v1.
3. WHEN Admin mengubah sebuah nilai pada App_Settings, THE API_Server SHALL menyimpan nilai baru, mengisi `updated_by` dan `updated_at`, dan mengubah `is_initial_value` menjadi false.
4. THE API_Contract_Doc SHALL menyertakan field `is_initial_value` pada setiap response pembacaan App_Settings.
5. THE Evaluation_Algorithm_Doc SHALL menandai setiap nilai default yang dibacanya dari App_Settings sebagai Initial_Value, bukan nilai final produk.
6. IF nilai baru sebuah parameter App_Settings gagal validasi tipe atau rentang yang didefinisikan, THEN THE API_Server SHALL menolak perubahan dengan HTTP 400 dan `code` `validation_error`.
7. WHEN sebuah Attempt atau evaluasi dihitung, THE Mastery_Engine SHALL memakai nilai parameter yang berlaku saat penghitungan, dan THE Mastery_Engine SHALL mencatat nilai parameter yang dipakai pada `evidence`.

### Requirement 25: Evaluation_Retry dan fallback unscored (Keputusan D-12)

**User Story:** As a Learner, I want mengulang evaluasi jawaban yang gagal dinilai tanpa mengetik ulang, so that jawaban saya tetap dihargai ketika AI sempat bermasalah.

#### Acceptance Criteria

1. THE Database_Schema_Doc SHALL menyimpan Answer_Evaluation_Status dengan nilai `pending`, `scored`, `failed`, dan `unscored` melalui constraint `CHECK`, beserta jumlah `evaluation_retry_count` per jawaban.
2. THE API_Contract_Doc SHALL mendefinisikan endpoint Evaluation_Retry untuk jawaban assessment dan jawaban interview yang hanya dapat dipanggil oleh pemilik jawaban.
3. WHEN Learner meminta Evaluation_Retry untuk jawaban berstatus `failed` dan `evaluation_retry_count` kurang dari `max_evaluation_retries` (App_Settings, Initial_Value 1, lihat ASUMSI A-25), THE Assessment_Service SHALL mengubah status menjadi `pending`, menaikkan `evaluation_retry_count` sebesar 1, dan membuat satu AITask `answer_evaluation` baru dengan Prompt_Contract yang sama dengan evaluasi sebelumnya.
4. THE Assessment_Service SHALL mempertahankan isi jawaban tersimpan tanpa perubahan selama Evaluation_Retry.
5. WHEN Evaluation_Retry diminta berulang untuk jawaban yang sudah berstatus `pending` atau `scored`, THE API_Server SHALL mengembalikan status evaluasi saat ini dengan HTTP 200 tanpa membuat AITask baru dan tanpa menaikkan `evaluation_retry_count`.
6. IF Evaluation_Retry diminta untuk jawaban berstatus `unscored`, THEN THE API_Server SHALL mengembalikan HTTP 409 dengan `code` `evaluation_not_retryable`.
7. WHEN Evaluation_Retry diminta, THE API_Server SHALL menerapkan Rate_Limiter kelas AI, Quota_Manager, dan `per_user_queue_limit` seperti pada AITask lain, dan mengembalikan `rate_limited`, `ai_quota_exceeded`, atau `ai_queue_limit_exceeded` sesuai batas yang terlampaui tanpa mengubah status jawaban.
8. IF evaluasi hasil Evaluation_Retry gagal dan `evaluation_retry_count` telah mencapai `max_evaluation_retries`, THEN THE AI_Gateway SHALL menetapkan status jawaban `unscored`.
9. WHEN sebuah jawaban berstatus `failed` melewati `evaluation_retry_window_hours` (App_Settings, Initial_Value 24) tanpa Evaluation_Retry yang berhasil, THE Backend_Lab SHALL menetapkan status jawaban `unscored` melalui job terjadwal (lihat ASUMSI A-24).
10. WHEN sebuah jawaban menjadi `unscored`, THE Mastery_Engine SHALL menghitung skor Attempt tanpa jawaban tersebut dan membiarkan `user_misconceptions` tanpa perubahan akibat jawaban itu.
11. THE API_Contract_Doc SHALL menampilkan Answer_Evaluation_Status dan `evaluation_retry_count` setiap jawaban pada response hasil assessment dan hasil interview.

### Requirement 26: Property_Test wajib untuk logika inti (Keputusan D-16)

**User Story:** As a backend engineer, I want property test yang wajib untuk logika inti, so that aturan deterministik terjaga terhadap seluruh ruang input dan bukan hanya contoh tertentu.

#### Acceptance Criteria

1. THE Evaluation_Algorithm_Doc SHALL mendefinisikan, untuk setiap area logika inti, daftar properti atau invariant yang wajib diuji dengan Property_Test: scoring free-text, Mastery v1, alokasi follow-up, skor interview, rekomendasi, token bucket dan backoff, aturan status dan lifecycle (Translation, Interview_Session, AITask, Answer_Evaluation_Status), dan invariant autentikasi token (rotation, reuse detection, revocation).
2. THE Spec_Set SHALL menandai Property_Test pada delapan area pada kriteria 1 sebagai wajib, bukan opsional.
3. THE Spec_Set SHALL mengizinkan pengujian di luar delapan area tersebut ditandai opsional.
4. THE Evaluation_Algorithm_Doc SHALL menetapkan fungsi inti pada delapan area tersebut sebagai fungsi murni tanpa akses jaringan, database, atau waktu sistem langsung, sehingga Property_Test dapat berjalan secara deterministik.
5. THE Spec_Set SHALL menetapkan bahwa Property_Test wajib berjalan pada pipeline CI.

## Open Decisions & Assumptions

### Keputusan user (tercatat sebagai keputusan, bukan asumsi)

- **D-1: Critical misconception.** Katalog `misconceptions` per Topic dikurasi Admin dengan kolom kode, deskripsi, `severity` (`minor`/`critical`), dan `topic_id`. Evaluator hanya boleh memilih dari katalog. Critical_Misconception yang belum resolved memblokir `MASTERED`. Golden_Set memakai `code` katalog. Definisi formal "critical" adalah `severity` `critical` pada entri katalog. Dicakup oleh Requirement 6, 13, 14, 17.
- **D-2: Bilingual.** Entitas induk menyimpan metadata non-teks. Tabel `*_translations` menyimpan field teks per Locale. Workflow status, versioning, dan reviewer per Locale berada di tabel translations. Aturan fallback Locale dan aturan "entitas hanya disajikan jika ada Published_Translation pada Locale yang diminta" ditetapkan. Dicakup oleh Requirement 3, 4, 5.
- **D-3: Autentikasi.** Email dan password, invite-only, JWT Access_Token dan Refresh_Token. Tabel `invitations` dan `refresh_tokens`. Register memerlukan Invite_Code buatan Admin. Refresh_Token disimpan sebagai hash dengan rotation dan revocation. Dicakup oleh Requirement 7 dan 8.
- **D-4: Role.** Product Spec v1.1 menetapkan role hanya `user` dan `admin`. Reviewer translation adalah referensi ke user `admin` pada `reviewer_id`. Dicakup oleh Requirement 4, 7, 10.
- **D-5: Parameter Product Spec v1.1.** Batas interview Practice dan Recruitment (total cap mengikat), lifecycle interview dan kolom minimum `interview_sessions`, Mastery v1 (3 Attempt, rata-rata 3 terakhir 80%, tanpa critical misconception unresolved, minimal satu assessment aplikatif atau berbasis skenario), bobot dimensi interview, status AITask beserta kebijakan token bucket, backoff, fallback, queue limit, dan prioritas, status konten (`draft`, `in_review`, `published`, `archived`), prioritas rekomendasi, pertahanan prompt injection, serta struktur Golden_Set. Dicakup oleh Requirement 11, 12, 13, 16, 17, 20, 21, 22, 23.
- **D-6: Status `PRACTICING` dan `NEEDS_REVIEW` (aturan sementara yang disetujui; sebelumnya A-20).** `NOT_STARTED` tanpa Attempt. `IN_PROGRESS` dengan 1 sampai 2 Attempt. `PRACTICING` dengan sedikitnya 3 Attempt yang tidak memenuhi `MASTERED` maupun `NEEDS_REVIEW`. `NEEDS_REVIEW` bila ada Unresolved_Critical_Misconception, atau flag `ever_mastered` true dan syarat `MASTERED` tidak terpenuhi, atau rata-rata 3 Attempt terakhir di bawah `needs_review_floor` (Initial_Value 50) pada sedikitnya 3 Attempt. Aturan ini disetujui sebagai aturan sementara dan dapat ditinjau ulang setelah ada data pemakaian. Dicakup oleh Requirement 12, 13.
- **D-7: Skor Attempt interview (sebelumnya A-19).** Attempt interview dihitung per Topic dari skor jawaban `scored` (pertanyaan utama dan follow-up digabung per pertanyaan utama). Dicakup oleh Requirement 21.
- **D-8: Skor dimensi interview.** `overall_score` dimensi interview dihitung terpisah dan tidak masuk ke skor Attempt maupun Mastery v1. Dicakup oleh Requirement 11, 21.
- **D-9: Bobot dimensi (sebelumnya A-22).** Seed 30/25/20/15/10 dipakai untuk `practice` dan `recruitment`, tersimpan per `interview_type` pada `interview_profiles` dan dapat diubah per tipe. Dicakup oleh Requirement 21.
- **D-10: Default scoring dan AI di App_Settings.** Penalti Misconception, clamp, ambang Golden_Set, kuota AI, batas antrean per user, dan TTL token disimpan di `app_settings` dan ditandai eksplisit sebagai Initial_Value melalui `is_initial_value`. Nilai pada A-10, A-11, A-14, dan A-15 adalah Initial_Value tersebut, bukan nilai final. Dicakup oleh Requirement 8, 16, 17, 24.
- **D-11: Jumlah Topic per Interview_Session (sementara; sebelumnya OD-5).** 1 sampai 5 Topic per sesi sebagai batas sementara, disimpan di App_Settings. Dicakup oleh Requirement 21.
- **D-12: Kegagalan evaluasi (sebelumnya OD-6).** Jawaban tersimpan boleh di-retry evaluasinya. Bila tetap gagal, status jawaban menjadi `unscored` dan skor Attempt dihitung tanpa jawaban itu. Mekanisme Evaluation_Retry bersifat idempoten, tidak mengubah jawaban tersimpan, dan tunduk pada Rate_Limiter dan kuota AI. Dicakup oleh Requirement 13, 14, 20, 21, 25.
- **D-13: Golden_Set di CI dan evaluasi berkala.** CI memakai response model yang direkam (`recorded`). Evaluasi berkala memakai model nyata (`live`) melalui trigger manual atau jadwal di luar CI. Dicakup oleh Requirement 15, 17.
- **D-14: Refresh_Token paralel.** Server tidak menyediakan grace window. Penanganan request paralel menjadi kewajiban klien melalui single-flight (satu request `refresh` untuk semua tab dan permintaan paralel). Deteksi reuse oleh server tetap me-revoke seluruh Token_Family. Dicakup oleh Requirement 8.
- **D-15: Penyimpanan queue dan rate limit.** PostgreSQL dipakai bersama untuk Job_Queue dan Rate_Limiter pada deployment multi-instance (default untuk multi-instance). Mode in-process hanya untuk single-instance atau dev. Dicakup oleh Requirement 16.
- **D-16: Property_Test wajib.** Property_Test wajib untuk scoring free-text, Mastery v1, alokasi follow-up, skor interview, rekomendasi, token bucket dan backoff, aturan status dan lifecycle, dan invariant autentikasi token. Pengujian lain boleh opsional. Dicakup oleh Requirement 26.

Catatan penomoran: A-19, A-20, A-22, dan A-17 dipindahkan menjadi keputusan (D-7, D-6, D-9, D-15). Nomor A tersebut tidak dipakai ulang. OD-5 dan OD-6 belum tercatat pada versi sebelumnya dari dokumen ini dan langsung dicatat sebagai D-11 dan D-12.

### Asumsi (ASUMSI)

- **A-1 (ASUMSI):** Nama produk "Backend Lab" adalah placeholder.
- **A-2 (ASUMSI):** Product Spec Backend Lab v1.1 dianggap sebagai sumber cakupan P0, P1, dan P2. Berkas tersebut tidak tersedia di workspace, sehingga hal di luar ringkasan yang diberikan user disusun dari pola umum dan harus direkonsiliasi (OD-1).
- **A-3 (ASUMSI):** Locale yang didukung adalah `id` dan `en`, dikonfigurasi sebagai daftar. Default_Locale adalah `id` dan dapat diubah melalui konfigurasi. Aturan "tanpa penggantian lintas Locale" pada Requirement 5 adalah interpretasi atas D-2: fallback berlaku pada penentuan Locale (tag regional ke bahasa dasar, lalu Default_Locale untuk Locale tidak didukung) dan bukan penggantian isi dengan Locale lain. Response `translation_not_available` menyertakan `available_locales` agar klien dapat menawarkan pergantian bahasa.
- **A-4 (ASUMSI):** Interview retry adalah Interview_Session baru yang dapat dibuat kapan saja, tanpa batas jumlah di MVP.
- **A-5 (ASUMSI):** Interview-Ready tidak didefinisikan. Hanya tersedia kolom `interview_ready` nullable tanpa rumus.
- **A-6 (ASUMSI):** Retention Transcript tanpa batas di MVP. Kolom `deleted_at` dan `retention_until` nullable disediakan untuk kebijakan di masa depan.
- **A-7 (ASUMSI):** AI tutoring dalam materials di luar cakupan. Hanya ekstensi melalui Task_Type baru.
- **A-8 (ASUMSI):** Primary key memakai UUID (v7 bila memungkinkan; fallback ke generator lain yang didokumentasikan). Timestamp memakai `timestamptz` UTC.
- **A-9 (ASUMSI):** Translation baru masuk sebagai `draft` pada `version` 1. Hanya `version` terkini yang disimpan, tanpa tabel riwayat versi.
- **A-10 (ASUMSI):** Default masa berlaku Access_Token 15 menit dan Refresh_Token 30 hari, keduanya disimpan di App_Settings. Nilai ini adalah Initial_Value yang dapat diubah (D-10).
- **A-11 (ASUMSI):** Nilai default parameter yang tidak ditetapkan Product Spec (bobot penalti `minor`, penalti dan batas skor Critical_Misconception, nilai kredit `partial`, `max_validation_retries`, ambang follow-up, ambang rekomendasi, dan ambang Golden_Set) ditetapkan di Evaluation_Algorithm_Doc sebagai Initial_Value yang disimpan di App_Settings dan dapat diubah (D-10). `mastered_threshold` 80 berasal dari Product Spec.
- **A-12 (ASUMSI, dikoreksi mengikuti Product Spec v1.1):** Role yang tersedia hanya `user` dan `admin`. Tidak ada role `learner` maupun `reviewer`. Kolom `reviewer_id` pada Translation merujuk ke user ber-role `admin`. Istilah Learner pada dokumen ini berarti user ber-role `user`.
- **A-13 (ASUMSI):** Kriteria Misconception `resolved` adalah: Evaluator melaporkan `code` tersebut pada `resolved_misconception_codes` pada Attempt berikutnya pada Topic yang sama, atau Admin menandai manual dengan alasan. Jumlah laporan berturut-turut yang diperlukan adalah parameter `resolve_consecutive_required` dengan default 1.
- **A-14 (ASUMSI):** Kuota AI harian dan bulanan tidak ditetapkan oleh Product Spec. Kuota disimpan di App_Settings dengan Initial_Value yang dapat diubah Admin (D-10).
- **A-15 (ASUMSI):** Golden_Set dimiliki Admin. `min_cases_per_category` adalah parameter App_Settings dengan Initial_Value 5 (D-10).
- **A-16 (ASUMSI):** API memakai prefix `/api/v1`. Daftar yang tumbuh memakai cursor pagination dan daftar admin kecil memakai offset pagination. Seluruh error memakai Error_Envelope.
- **A-18 (ASUMSI):** Cakupan P0 penuh. RAG/pgvector (P1) adalah migrasi terpisah. P2 hanya ekstensi.
- **A-21 (ASUMSI):** Syarat "minimal 1 assessment aplikatif atau berbasis skenario" dievaluasi pada jendela 3 Attempt terakhir (parameter `application_evidence_scope`, nilai `window` atau `all`, default `window`).
- **A-23 (ASUMSI):** Pertanyaan utama interview diambil dari bank Question `published` (purpose `interview` atau `both`), sedangkan follow-up disusun AI melalui Task_Type `interview_followup`. Interview dapat mencakup 1 sampai 5 Topic (D-11).
- **A-24 (ASUMSI):** Jawaban berstatus `failed` yang tidak di-retry berhasil dalam `evaluation_retry_window_hours` (Initial_Value 24) otomatis menjadi `unscored`, agar pembuatan `topic_attempts` tidak tertunda tanpa batas. Batas waktu ini belum diputuskan user.
- **A-25 (ASUMSI):** `max_evaluation_retries` memiliki Initial_Value 1, yaitu satu kali retry setelah kegagalan awal, sebelum jawaban menjadi `unscored`. Nilai ini belum diputuskan user.
- **A-26 (ASUMSI):** Run Golden_Set `live` merekam response model sehingga run `recorded` pada CI dapat memakainya (Requirement 17.14). Mekanisme perekaman belum diputuskan user.

### Open Items

- **OD-1:** Rekonsiliasi dengan teks Product Spec Backend Lab v1.1 yang lengkap: (a) detail field teks per entitas konten, (b) fitur P1 dan P2 spesifik. Butir bobot dimensi per `interview_type`, kriteria status `PRACTICING` dan `NEEDS_REVIEW`, dan definisi Attempt interview sudah diputuskan (D-9, D-6, D-7); rekonsiliasi hanya perlu memeriksa kesesuaian dengan Product Spec.
- **OD-2:** Strategi pemilihan Locale untuk Learner yang belum memiliki `preferred_locale`, bila Product Spec menetapkan aturan berbeda dari urutan pada Requirement 5.1.
