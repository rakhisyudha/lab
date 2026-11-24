INSERT INTO supported_locales (code, display_name, is_default) VALUES ('id','Bahasa Indonesia', true), ('en','English', false);

INSERT INTO ai_task_types (task_type, description, default_priority) VALUES
 ('answer_evaluation',    'Menilai satu jawaban free-text terhadap Concept dan katalog Misconception', 2),
 ('interview_followup',   'Menyusun teks pertanyaan follow-up interview',                              1),
 ('interview_evaluation', 'Menilai lima dimensi interview pada akhir sesi',                            2);
-- priority 1 = interview aktif, 2 = evaluasi jawaban assessment dan evaluasi akhir, 3 = background generation.
-- answer_evaluation untuk jawaban interview di-enqueue dengan priority 1 oleh interview.Orchestrator.

INSERT INTO interview_profiles
 (interview_type, min_questions, max_questions, max_followups_per_question, total_cap, max_depth,
  w_technical_accuracy, w_conceptual_understanding, w_reasoning, w_practical_application, w_communication)
VALUES
 ('practice',    5, 10, 2, 10, 2, 30, 25, 20, 15, 10),
 ('recruitment', 6, 10, 3, 15, 3, 30, 25, 20, 15, 10);   -- D-9: seed bobot 30/25/20/15/10 sama untuk kedua tipe; dapat diubah per tipe

-- Seed app_settings (Requirement 24.2). Semua baris berstatus is_initial_value = true (default kolom):
-- nilai di bawah adalah Initial_Value, bukan nilai final produk. Kolom: key, value, value_schema, description.
-- Validasi lintas kunci (dijalankan API sebelum menyimpan): scoring.clamp_min < scoring.clamp_max;
-- scoring.critical_score_cap berada di antara clamp_min dan clamp_max; interview.min_topics_per_session <= interview.max_topics_per_session.
INSERT INTO app_settings (key, value, value_schema, description) VALUES
 -- Mastery v1 (D-6; mastered_threshold, min_attempts, window_size berasal dari Product Spec)
 ('mastery.mastered_threshold',          '80',       '{"type":"number","minimum":0,"maximum":100}',      'Ambang rata-rata 3 Attempt terakhir untuk MASTERED'),
 ('mastery.min_attempts',                '3',        '{"type":"integer","minimum":1}',                   'Jumlah Attempt minimum untuk MASTERED, PRACTICING, NEEDS_REVIEW'),
 ('mastery.window_size',                 '3',        '{"type":"integer","minimum":1}',                   'Jumlah Attempt terakhir pada jendela skor'),
 ('mastery.needs_review_floor',          '50',       '{"type":"number","minimum":0,"maximum":100}',      'Rata-rata di bawah nilai ini (n >= min_attempts) menjadi NEEDS_REVIEW'),
 ('mastery.application_evidence_scope',  '"window"', '{"enum":["window","all"]}',                        'Cakupan bukti Attempt aplikatif'),
 ('mastery.resolve_consecutive_required','1',        '{"type":"integer","minimum":1}',                   'Laporan resolved berturut-turut untuk menutup Misconception'),
 -- Scoring free-text
 ('scoring.partial_credit',              '0.5',      '{"type":"number","minimum":0,"maximum":1}',        'Nilai kredit Concept berstatus partial'),
 ('scoring.minor_penalty',               '5',        '{"type":"number","minimum":0,"maximum":100}',      'Penalti poin per Misconception minor'),
 ('scoring.minor_penalty_cap',           '15',       '{"type":"number","minimum":0,"maximum":100}',      'Batas total penalti Misconception minor'),
 ('scoring.critical_penalty',            '15',       '{"type":"number","minimum":0,"maximum":100}',      'Penalti poin per Critical_Misconception'),
 ('scoring.critical_score_cap',          '60',       '{"type":"number","minimum":0,"maximum":100}',      'Skor jawaban maksimum bila ada Critical_Misconception'),
 ('scoring.clamp_min',                   '0',        '{"type":"number","minimum":0,"maximum":50}',       'Batas bawah clamp skor jawaban'),
 ('scoring.clamp_max',                   '100',      '{"type":"number","minimum":50,"maximum":100}',     'Batas atas clamp skor jawaban'),
 ('scoring.hint_divergence_threshold',   '30',       '{"type":"number","minimum":0,"maximum":100}',      'Selisih score_hint dan skor server yang menandai needs_review'),
 ('scoring.low_confidence_threshold',    '0.5',      '{"type":"number","minimum":0,"maximum":1}',        'Confidence di bawah nilai ini menandai needs_review'),
 -- AI: validasi, retry evaluasi, antrean, provider
 ('ai.max_validation_retries',           '2',        '{"type":"integer","minimum":0,"maximum":5}',       'Percobaan ulang saat output gagal validasi'),
 ('ai.max_evaluation_retries',           '1',        '{"type":"integer","minimum":0,"maximum":5}',       'Evaluation_Retry per jawaban sebelum menjadi unscored (A-25)'),
 ('ai.evaluation_retry_window_hours',    '24',       '{"type":"integer","minimum":1,"maximum":720}',     'Jam sejak kegagalan pertama sebelum jawaban failed menjadi unscored (A-24)'),
 ('ai.per_user_queue_limit',             '20',       '{"type":"integer","minimum":1}',                   'Maksimum AITask queued dan processing per user'),
 ('ai.lock_timeout_seconds',             '120',      '{"type":"integer","minimum":10}',                  'Batas lock AITask processing sebelum dikembalikan ke queued'),
 ('ai.provider_max_attempts',            '2',        '{"type":"integer","minimum":1}',                   'Percobaan per provider sebelum fallback'),
 ('ai.backoff_base_seconds',             '2',        '{"type":"number","minimum":0.1}',                  'Dasar exponential backoff'),
 ('ai.backoff_cap_seconds',              '60',       '{"type":"number","minimum":1}',                    'Batas atas backoff'),
 -- Kuota AI: ai.quota.{scope}.{period}.{metric}; value null = tanpa batas (A-14)
 ('ai.quota.user.daily.requests',        '200',      '{"type":["integer","null"],"minimum":1}',          'Kuota request per user per hari (UTC)'),
 ('ai.quota.user.monthly.total_tokens',  '2000000',  '{"type":["integer","null"],"minimum":1}',          'Kuota token per user per bulan (UTC)'),
 ('ai.quota.global.daily.total_tokens',  '20000000', '{"type":["integer","null"],"minimum":1}',          'Kuota token global per hari (UTC)'),
 -- Lima kombinasi sisanya disemai null (tanpa batas) agar seluruh delapan kombinasi scope x period x metric dapat diubah Admin,
 -- karena kunci baru tidak dapat dibuat lewat API (DA-19).
 ('ai.quota.user.daily.total_tokens',    'null',     '{"type":["integer","null"],"minimum":1}',          'Kuota token per user per hari (UTC), null = tanpa batas'),
 ('ai.quota.user.monthly.requests',      'null',     '{"type":["integer","null"],"minimum":1}',          'Kuota request per user per bulan (UTC), null = tanpa batas'),
 ('ai.quota.global.daily.requests',      'null',     '{"type":["integer","null"],"minimum":1}',          'Kuota request global per hari (UTC), null = tanpa batas'),
 ('ai.quota.global.monthly.requests',    'null',     '{"type":["integer","null"],"minimum":1}',          'Kuota request global per bulan (UTC), null = tanpa batas'),
 ('ai.quota.global.monthly.total_tokens','null',     '{"type":["integer","null"],"minimum":1}',          'Kuota token global per bulan (UTC), null = tanpa batas'),
 -- TTL token (A-10). Perubahan hanya berlaku untuk token yang diterbitkan setelahnya
 ('auth.access_token_ttl_seconds',       '900',      '{"type":"integer","minimum":60,"maximum":86400}',   'Masa berlaku Access_Token (15 menit)'),
 ('auth.refresh_token_ttl_seconds',      '2592000',  '{"type":"integer","minimum":3600,"maximum":31536000}', 'Masa berlaku Refresh_Token (30 hari)'),
 -- Interview
 ('interview.min_topics_per_session',    '1',        '{"type":"integer","minimum":1}',                   'Minimum Topic per Interview_Session (D-11, sementara)'),
 ('interview.max_topics_per_session',    '5',        '{"type":"integer","minimum":1}',                   'Maksimum Topic per Interview_Session (D-11, sementara)'),
 ('interview.pause_ttl_days',            '7',        '{"type":"integer","minimum":1}',                   'Masa sesi PAUSED sebelum ABANDONED'),
 ('followup.trigger_score',              '70',       '{"type":"number","minimum":0,"maximum":100}',      'Skor di bawah nilai ini memicu pertimbangan follow-up'),
 ('followup.base_cutoff',                '30',       '{"type":"number","minimum":0,"maximum":100}',      'Dasar ambang kebutuhan follow-up'),
 ('followup.scarcity_weight',            '40',       '{"type":"number","minimum":0,"maximum":100}',      'Bobot kelangkaan anggaran pertanyaan'),
 ('followup.critical_bonus',             '20',       '{"type":"number","minimum":0,"maximum":100}',      'Tambahan kebutuhan bila ada Critical_Misconception'),
 -- Rekomendasi
 ('recommendation.low_mastery_threshold','60',       '{"type":"number","minimum":0,"maximum":100}',      'Ambang avg_last3 untuk rule LOW_MASTERY'),
 ('recommendation.limit',                '5',        '{"type":"integer","minimum":1,"maximum":50}',      'Jumlah rekomendasi default'),
 -- Golden_Set (ambang lulus; catalog_violation_count dan schema_violation_count tetap 0 dan tidak dapat diubah)
 ('golden.min_cases_per_category',       '5',        '{"type":"integer","minimum":1}',                   'Kasus aktif minimum per kategori (A-15)'),
 ('golden.regression_tolerance',         '0.03',     '{"type":"number","minimum":0,"maximum":1}',        'Toleransi penurunan metrik terhadap baseline live'),
 ('golden.threshold.score_in_range_rate','0.85',     '{"type":"number","minimum":0,"maximum":1}',        'Ambang minimum score_in_range_rate'),
 ('golden.threshold.concept_f1',         '0.80',     '{"type":"number","minimum":0,"maximum":1}',        'Ambang minimum concept_f1'),
 ('golden.threshold.critical_recall',    '0.95',     '{"type":"number","minimum":0,"maximum":1}',        'Ambang minimum critical_recall'),
 ('golden.threshold.false_critical_rate','0.05',     '{"type":"number","minimum":0,"maximum":1}',        'Ambang maksimum false_critical_rate'),
 ('golden.threshold.injection_resilience_rate','1.0','{"type":"number","minimum":0,"maximum":1}',        'Ambang minimum injection_resilience_rate'),
 ('golden.live_schedule_enabled',        'false',    '{"type":"boolean"}',                               'Aktifkan run live berkala terhadap Prompt_Contract aktif'),
 ('golden.live_schedule_cron',           '"0 2 * * 1"','{"type":"string","maxLength":64}',               'Jadwal run live berkala (cron UTC); di luar pipeline CI')
ON CONFLICT (key) DO NOTHING;

-- Contoh provider (nonaktif sampai kunci API dikonfigurasi di environment)
INSERT INTO ai_provider_settings (provider_name, model_name, fallback_order, rpm_limit, tpm_limit, is_enabled) VALUES
 ('primary',  'configure-me', 1, 60, 100000, false);
