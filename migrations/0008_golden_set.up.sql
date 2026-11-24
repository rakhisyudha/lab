CREATE TABLE golden_cases (
  id                           uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  question_id                  uuid NOT NULL REFERENCES questions(id) ON DELETE RESTRICT,
  locale                       text NOT NULL REFERENCES supported_locales(code),
  category                     text NOT NULL CHECK (category IN
                                  ('different_wording','partially_correct','critical_misconception',
                                   'off_topic_or_empty','adversarial_injection')),
  sample_answer                text NOT NULL CHECK (char_length(sample_answer) <= 8000),   -- boleh '' untuk kasus kosong
  expected_score_min           numeric(5,2) NOT NULL CHECK (expected_score_min BETWEEN 0 AND 100),
  expected_score_max           numeric(5,2) NOT NULL CHECK (expected_score_max BETWEEN 0 AND 100),
  expected_found_concept_codes text[] NOT NULL DEFAULT '{}',
  expected_missed_concept_codes text[] NOT NULL DEFAULT '{}',
  expected_misconception_codes text[] NOT NULL DEFAULT '{}',
  notes                        text NOT NULL DEFAULT '',
  owner_id                     uuid NOT NULL REFERENCES users(id) ON DELETE RESTRICT,
  is_active                    boolean NOT NULL DEFAULT true,
  created_at                   timestamptz NOT NULL DEFAULT now(),
  updated_at                   timestamptz NOT NULL DEFAULT now(),
  CONSTRAINT golden_cases_range_chk CHECK (expected_score_min <= expected_score_max),
  CONSTRAINT golden_cases_disjoint_chk CHECK (NOT (expected_found_concept_codes && expected_missed_concept_codes))
);
CREATE INDEX golden_cases_active_idx ON golden_cases (category) WHERE is_active;
CREATE INDEX golden_cases_question_idx ON golden_cases (question_id);
SELECT attach_updated_at_trigger('golden_cases');

-- run_mode (Requirement 17.3, 17.12, 17.13):
--   recorded = menilai response model yang direkam, tanpa AI provider; dipakai pada pipeline CI.
--   live     = memanggil model nyata lewat AI_Gateway; manual oleh Admin atau terjadwal di luar CI.
-- Hanya run 'live' yang dihitung sebagai baseline regresi dan syarat aktivasi Prompt_Contract (Requirement 17.15).
CREATE TABLE golden_runs (
  id                 uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  prompt_contract_id uuid NOT NULL REFERENCES prompt_contracts(id) ON DELETE RESTRICT,
  run_mode           text NOT NULL CHECK (run_mode IN ('recorded','live')),
  baseline_run_id    uuid REFERENCES golden_runs(id) ON DELETE SET NULL,   -- wajib run_mode='live' dan status='passed' (dijaga aplikasi, diuji integrasi)
  model_name         text NOT NULL,
  status             text NOT NULL DEFAULT 'running'
                       CHECK (status IN ('running','passed','failed','insufficient_coverage','errored')),
  error_reasons      text[] NOT NULL DEFAULT '{}',          -- mis. recording_missing:<case_id>, recording_stale:<case_id>
  trigger_source     text NOT NULL DEFAULT 'manual' CHECK (trigger_source IN ('manual','schedule','ci')),
  metrics            jsonb NOT NULL DEFAULT '{}'::jsonb,
  thresholds         jsonb NOT NULL DEFAULT '{}'::jsonb,   -- snapshot ambang saat run
  coverage           jsonb NOT NULL DEFAULT '{}'::jsonb,   -- {category: jumlah kasus aktif}
  regression         jsonb NOT NULL DEFAULT '{}'::jsonb,   -- hasil perbandingan terhadap baseline
  triggered_by       uuid REFERENCES users(id) ON DELETE SET NULL,
  started_at         timestamptz NOT NULL DEFAULT now(),
  finished_at        timestamptz,
  created_at         timestamptz NOT NULL DEFAULT now(),
  updated_at         timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX golden_runs_contract_idx ON golden_runs (prompt_contract_id, started_at DESC);
-- run live terbaru per kontrak (dasar gate aktivasi dan pemilihan baseline)
CREATE INDEX golden_runs_live_idx ON golden_runs (prompt_contract_id, started_at DESC) WHERE run_mode = 'live';
SELECT attach_updated_at_trigger('golden_runs');

ALTER TABLE prompt_contracts
  ADD CONSTRAINT prompt_contracts_golden_fk FOREIGN KEY (activated_golden_run_id)
  REFERENCES golden_runs(id) ON DELETE SET NULL;

CREATE TABLE golden_run_results (
  id                  uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  golden_run_id       uuid NOT NULL REFERENCES golden_runs(id) ON DELETE CASCADE,
  golden_case_id      uuid NOT NULL REFERENCES golden_cases(id) ON DELETE RESTRICT,
  schema_valid        boolean NOT NULL,
  catalog_violation   boolean NOT NULL DEFAULT false,
  actual_score        numeric(5,2) CHECK (actual_score BETWEEN 0 AND 100),
  actual_found_codes  text[] NOT NULL DEFAULT '{}',
  actual_missed_codes text[] NOT NULL DEFAULT '{}',
  actual_misconception_codes text[] NOT NULL DEFAULT '{}',
  injection_obeyed    boolean NOT NULL DEFAULT false,
  checks              jsonb NOT NULL DEFAULT '{}'::jsonb,   -- {score_in_range, found_ok, missed_ok, misconception_ok}
  passed              boolean NOT NULL,
  raw_output          jsonb,
  created_at          timestamptz NOT NULL DEFAULT now(),
  CONSTRAINT golden_run_results_uk UNIQUE (golden_run_id, golden_case_id)
);

-- Rekaman response model (A-26, Requirement 17.14). Run 'live' menulis satu baris per (prompt_contract_id, model_name, kasus);
-- run 'recorded' membacanya lewat provider replay tanpa memanggil AI provider.
-- 'responses' menyimpan urutan seluruh percobaan model pada satu kasus (termasuk percobaan yang gagal validasi),
-- sehingga replay mereproduksi perilaku retry validasi gateway.
CREATE TABLE golden_recordings (
  id                 uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  prompt_contract_id uuid NOT NULL REFERENCES prompt_contracts(id) ON DELETE RESTRICT,
  model_name         text NOT NULL,
  golden_case_id     uuid NOT NULL REFERENCES golden_cases(id) ON DELETE CASCADE,
  case_sha256        text NOT NULL,       -- hash kanonik (pertanyaan, Concept, katalog, sample_answer, locale); berubah = rekaman basi
  responses          jsonb NOT NULL,      -- [{"raw_text": "...", "input_tokens": 812, "output_tokens": 240}, ...]
  source_run_id      uuid NOT NULL REFERENCES golden_runs(id) ON DELETE CASCADE,
  recorded_at        timestamptz NOT NULL DEFAULT now(),
  CONSTRAINT golden_recordings_uk UNIQUE (prompt_contract_id, model_name, golden_case_id),
  CONSTRAINT golden_recordings_resp_chk CHECK (jsonb_typeof(responses) = 'array' AND jsonb_array_length(responses) >= 1)
);
CREATE INDEX golden_recordings_source_idx ON golden_recordings (source_run_id);
