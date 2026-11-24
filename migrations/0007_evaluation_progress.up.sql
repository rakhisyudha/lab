CREATE TABLE answer_evaluations (
  id                    uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  user_id               uuid NOT NULL REFERENCES users(id) ON DELETE RESTRICT,
  topic_id              uuid NOT NULL REFERENCES topics(id) ON DELETE RESTRICT,
  question_id           uuid NOT NULL REFERENCES questions(id) ON DELETE RESTRICT,
  locale                text NOT NULL REFERENCES supported_locales(code),
  assessment_answer_id  uuid REFERENCES assessment_answers(id) ON DELETE CASCADE,
  interview_message_id  uuid REFERENCES interview_messages(id) ON DELETE CASCADE,
  ai_task_id            uuid REFERENCES ai_tasks(id) ON DELETE SET NULL,
  prompt_contract_id    uuid REFERENCES prompt_contracts(id) ON DELETE RESTRICT,
  model_name            text,
  -- Baris hanya dibuat untuk jawaban yang berhasil dinilai (evaluation_status 'scored' pada jawaban sumber).
  -- Kegagalan tidak membuat baris: jejaknya ada pada ai_tasks, ai_usage_logs, dan status jawaban.
  kind                  text NOT NULL DEFAULT 'ai' CHECK (kind IN ('ai','blank')),   -- 'blank' = jawaban kosong, skor 0 tanpa AI
  relevance             text CHECK (relevance IN ('on_topic','off_topic','empty')),
  rubric_score          numeric(5,2) CHECK (rubric_score BETWEEN 0 AND 100),
  score_pct             numeric(5,2) CHECK (score_pct BETWEEN 0 AND 100),
  score_hint            numeric(5,2),                         -- masukan AI mentah, tidak dipakai untuk skor final
  concept_results       jsonb NOT NULL DEFAULT '[]'::jsonb,   -- [{concept_code, coverage, evidence}]
  penalties             jsonb NOT NULL DEFAULT '{}'::jsonb,   -- {minor:[codes], critical:[codes], total_points, capped}
  feedback              text NOT NULL DEFAULT '',
  confidence            numeric(3,2) CHECK (confidence BETWEEN 0 AND 1),
  needs_review          boolean NOT NULL DEFAULT false,       -- divergensi score_hint atau confidence rendah
  injection_suspected   boolean NOT NULL DEFAULT false,
  validation_attempts   smallint NOT NULL DEFAULT 1,
  params_snapshot       jsonb NOT NULL DEFAULT '{}'::jsonb,   -- nilai parameter app_settings yang dipakai (Requirement 24.7)
  raw_output            jsonb,
  created_at            timestamptz NOT NULL DEFAULT now(),
  CONSTRAINT answer_evaluations_source_chk CHECK (num_nonnulls(assessment_answer_id, interview_message_id) = 1),
  CONSTRAINT answer_evaluations_score_chk  CHECK (score_pct IS NOT NULL),
  CONSTRAINT answer_evaluations_blank_chk  CHECK (kind = 'ai' OR (relevance = 'empty' AND prompt_contract_id IS NULL))
);
CREATE UNIQUE INDEX answer_evaluations_aa_uk ON answer_evaluations (assessment_answer_id) WHERE assessment_answer_id IS NOT NULL;
CREATE UNIQUE INDEX answer_evaluations_im_uk ON answer_evaluations (interview_message_id) WHERE interview_message_id IS NOT NULL;
CREATE INDEX answer_evaluations_user_topic_idx ON answer_evaluations (user_id, topic_id, created_at DESC);

CREATE TABLE answer_evaluation_misconceptions (
  answer_evaluation_id uuid NOT NULL REFERENCES answer_evaluations(id) ON DELETE CASCADE,
  misconception_id     uuid NOT NULL REFERENCES misconceptions(id) ON DELETE RESTRICT,
  kind                 text NOT NULL CHECK (kind IN ('detected','resolved')),
  PRIMARY KEY (answer_evaluation_id, misconception_id)      -- tidak boleh detected dan resolved bersamaan
);
CREATE INDEX aem_misconception_idx ON answer_evaluation_misconceptions (misconception_id);

-- Evaluasi dimensi akhir sesi. overall_score adalah skor dimensi dan terpisah dari skor Attempt (D-8, Requirement 21.9):
-- nilai ini tidak pernah menjadi masukan topic_attempts.score_pct maupun Mastery v1.
-- Kolom status di sini adalah status evaluasi sesi, bukan Answer_Evaluation_Status per jawaban.
CREATE TABLE interview_evaluations (
  id                   uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  interview_session_id uuid NOT NULL UNIQUE REFERENCES interview_sessions(id) ON DELETE CASCADE,
  status               text NOT NULL CHECK (status IN ('succeeded','failed')),
  dimension_scores     jsonb,           -- {"technical_accuracy": {"score": 80, "rationale": "..."}, ...}
  weights              jsonb,           -- bobot yang dipakai (snapshot dari configuration)
  overall_score        numeric(5,2) CHECK (overall_score BETWEEN 0 AND 100),
  summary              text NOT NULL DEFAULT '',
  strengths            jsonb NOT NULL DEFAULT '[]'::jsonb,
  improvements         jsonb NOT NULL DEFAULT '[]'::jsonb,
  needs_review         boolean NOT NULL DEFAULT false,
  prompt_contract_id   uuid REFERENCES prompt_contracts(id) ON DELETE RESTRICT,
  model_name           text,
  ai_task_id           uuid REFERENCES ai_tasks(id) ON DELETE SET NULL,
  raw_output           jsonb,
  created_at           timestamptz NOT NULL DEFAULT now(),
  CONSTRAINT interview_evaluations_ok_chk CHECK (status <> 'succeeded' OR (dimension_scores IS NOT NULL AND overall_score IS NOT NULL))
);

-- Ledger Attempt. Baris hanya dibuat bila sedikitnya satu jawaban pada Topic tersebut berstatus 'scored'.
-- Attempt assessment yang seluruh jawaban non-kosongnya 'unscored' dan Topic interview yang seluruh jawabannya 'unscored'
-- tidak menghasilkan baris (Requirement 20.11, 21.11). score_pct dihitung hanya dari jawaban 'scored'.
CREATE TABLE topic_attempts (
  id                       uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  user_id                  uuid NOT NULL REFERENCES users(id) ON DELETE RESTRICT,
  topic_id                 uuid NOT NULL REFERENCES topics(id) ON DELETE RESTRICT,
  source_type              text NOT NULL CHECK (source_type IN ('assessment','interview')),
  assessment_attempt_id    uuid REFERENCES assessment_attempts(id) ON DELETE RESTRICT,
  interview_session_id     uuid REFERENCES interview_sessions(id) ON DELETE RESTRICT,
  score_pct                numeric(5,2) NOT NULL CHECK (score_pct BETWEEN 0 AND 100),
  has_application_question boolean NOT NULL,
  scored_at                timestamptz NOT NULL,
  created_at               timestamptz NOT NULL DEFAULT now(),
  CONSTRAINT topic_attempts_source_chk CHECK (
       (source_type = 'assessment' AND assessment_attempt_id IS NOT NULL AND interview_session_id IS NULL)
    OR (source_type = 'interview'  AND interview_session_id  IS NOT NULL AND assessment_attempt_id IS NULL)),
  CONSTRAINT topic_attempts_aa_uk UNIQUE (assessment_attempt_id),
  CONSTRAINT topic_attempts_is_uk UNIQUE (interview_session_id, topic_id)
);
CREATE INDEX topic_attempts_window_idx ON topic_attempts (user_id, topic_id, scored_at DESC, id DESC);

CREATE TABLE mastery_records (
  id              uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  user_id         uuid NOT NULL REFERENCES users(id) ON DELETE RESTRICT,
  topic_id        uuid NOT NULL REFERENCES topics(id) ON DELETE RESTRICT,
  status          text NOT NULL DEFAULT 'NOT_STARTED'
                    CHECK (status IN ('NOT_STARTED','IN_PROGRESS','PRACTICING','NEEDS_REVIEW','MASTERED')),
  attempt_count   integer NOT NULL DEFAULT 0 CHECK (attempt_count >= 0),
  avg_last3       numeric(5,2) CHECK (avg_last3 BETWEEN 0 AND 100),
  best_score      numeric(5,2) CHECK (best_score BETWEEN 0 AND 100),
  last_attempt_id uuid REFERENCES topic_attempts(id) ON DELETE SET NULL,
  mastered_at     timestamptz,
  interview_ready boolean,                                  -- nullable, tanpa rumus (A-5)
  evidence        jsonb NOT NULL DEFAULT '{}'::jsonb,
  rule_version    text NOT NULL DEFAULT 'mastery_v1',      -- aturan sementara yang disetujui (D-6); ambang dari app_settings
  created_at      timestamptz NOT NULL DEFAULT now(),
  updated_at      timestamptz NOT NULL DEFAULT now(),
  CONSTRAINT mastery_records_user_topic_uk UNIQUE (user_id, topic_id),
  CONSTRAINT mastery_records_mastered_chk CHECK ((status = 'MASTERED') = (mastered_at IS NOT NULL))
);
CREATE INDEX mastery_records_user_status_idx ON mastery_records (user_id, status);
SELECT attach_updated_at_trigger('mastery_records');

CREATE TABLE user_misconceptions (
  id                       uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  user_id                  uuid NOT NULL REFERENCES users(id) ON DELETE RESTRICT,
  misconception_id         uuid NOT NULL REFERENCES misconceptions(id) ON DELETE RESTRICT,
  source_evaluation_id     uuid REFERENCES answer_evaluations(id) ON DELETE SET NULL,   -- evaluasi yang terakhir mendeteksi
  status                   text NOT NULL DEFAULT 'detected' CHECK (status IN ('detected','resolved')),
  detected_at              timestamptz NOT NULL DEFAULT now(),
  resolved_at              timestamptz,
  resolved_by_evaluation_id uuid REFERENCES answer_evaluations(id) ON DELETE SET NULL,
  resolved_by              uuid REFERENCES users(id) ON DELETE SET NULL,                -- admin pada resolusi manual
  resolution_reason        text,
  consecutive_resolve_count smallint NOT NULL DEFAULT 0 CHECK (consecutive_resolve_count >= 0),
  occurrences              integer NOT NULL DEFAULT 1 CHECK (occurrences >= 1),
  created_at               timestamptz NOT NULL DEFAULT now(),
  updated_at               timestamptz NOT NULL DEFAULT now(),
  CONSTRAINT user_misconceptions_uk UNIQUE (user_id, misconception_id),
  CONSTRAINT user_misconceptions_resolved_chk CHECK ((status = 'resolved') = (resolved_at IS NOT NULL)),
  CONSTRAINT user_misconceptions_manual_chk CHECK (resolved_by IS NULL OR resolution_reason IS NOT NULL)
);
CREATE INDEX user_misconceptions_open_idx ON user_misconceptions (user_id) WHERE status = 'detected';
SELECT attach_updated_at_trigger('user_misconceptions');
