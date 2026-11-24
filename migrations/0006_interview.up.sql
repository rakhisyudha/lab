-- Profil batas interview (Requirement 21.1); disalin ke sessions.configuration saat sesi dibuat
CREATE TABLE interview_profiles (
  interview_type             text PRIMARY KEY CHECK (interview_type IN ('practice','recruitment')),
  min_questions              smallint NOT NULL CHECK (min_questions >= 1),
  max_questions              smallint NOT NULL,
  max_followups_per_question smallint NOT NULL CHECK (max_followups_per_question >= 0),
  total_cap                  smallint NOT NULL,
  max_depth                  smallint NOT NULL CHECK (max_depth >= 0),
  w_technical_accuracy       smallint NOT NULL CHECK (w_technical_accuracy BETWEEN 0 AND 100),
  w_conceptual_understanding smallint NOT NULL CHECK (w_conceptual_understanding BETWEEN 0 AND 100),
  w_reasoning                smallint NOT NULL CHECK (w_reasoning BETWEEN 0 AND 100),
  w_practical_application    smallint NOT NULL CHECK (w_practical_application BETWEEN 0 AND 100),
  w_communication            smallint NOT NULL CHECK (w_communication BETWEEN 0 AND 100),
  created_at                 timestamptz NOT NULL DEFAULT now(),
  updated_at                 timestamptz NOT NULL DEFAULT now(),
  CONSTRAINT interview_profiles_q_chk   CHECK (min_questions <= max_questions),
  CONSTRAINT interview_profiles_cap_chk CHECK (total_cap >= max_questions),
  CONSTRAINT interview_profiles_w_chk   CHECK (w_technical_accuracy + w_conceptual_understanding + w_reasoning
                                               + w_practical_application + w_communication = 100)
);
SELECT attach_updated_at_trigger('interview_profiles');

CREATE TABLE interview_sessions (
  id                       uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  user_id                  uuid NOT NULL REFERENCES users(id) ON DELETE RESTRICT,
  status                   text NOT NULL DEFAULT 'CREATED'
                             CHECK (status IN ('CREATED','IN_PROGRESS','PAUSED','COMPLETED','ABANDONED')),
  interview_type           text NOT NULL REFERENCES interview_profiles(interview_type),
  configuration            jsonb NOT NULL,
  current_question_id      uuid REFERENCES questions(id) ON DELETE RESTRICT,
  current_question_index   smallint NOT NULL DEFAULT 0 CHECK (current_question_index >= 0),
  session_state            jsonb NOT NULL DEFAULT '{}'::jsonb,
  locale                   text NOT NULL REFERENCES supported_locales(code),
  retry_of_session_id      uuid REFERENCES interview_sessions(id) ON DELETE SET NULL,
  prompt_contract_versions jsonb NOT NULL DEFAULT '{}'::jsonb,   -- {"answer_evaluation":1,"interview_followup":1,"interview_evaluation":1}
  started_at               timestamptz,
  paused_at                timestamptz,
  expires_at               timestamptz,
  completed_at             timestamptz,
  abandoned_at             timestamptz,
  retention_until          timestamptz,
  deleted_at               timestamptz,
  created_at               timestamptz NOT NULL DEFAULT now(),
  updated_at               timestamptz NOT NULL DEFAULT now(),
  CONSTRAINT interview_sessions_cfg_chk     CHECK (jsonb_typeof(configuration) = 'object' AND jsonb_typeof(session_state) = 'object'),
  CONSTRAINT interview_sessions_paused_chk  CHECK (status <> 'PAUSED' OR (paused_at IS NOT NULL AND expires_at IS NOT NULL)),
  CONSTRAINT interview_sessions_active_chk  CHECK (status NOT IN ('CREATED','IN_PROGRESS') OR (paused_at IS NULL AND expires_at IS NULL)),
  CONSTRAINT interview_sessions_started_chk CHECK (status NOT IN ('IN_PROGRESS','PAUSED','COMPLETED') OR started_at IS NOT NULL),
  CONSTRAINT interview_sessions_done_chk    CHECK (status <> 'COMPLETED' OR completed_at IS NOT NULL),
  CONSTRAINT interview_sessions_aband_chk   CHECK (status <> 'ABANDONED' OR abandoned_at IS NOT NULL)
);
CREATE INDEX interview_sessions_user_idx ON interview_sessions (user_id, created_at DESC, id DESC) WHERE deleted_at IS NULL;
CREATE INDEX interview_sessions_expiry_idx ON interview_sessions (expires_at) WHERE status = 'PAUSED';
CREATE INDEX interview_sessions_retention_idx ON interview_sessions (retention_until) WHERE retention_until IS NOT NULL AND deleted_at IS NULL;
CREATE INDEX interview_sessions_retry_idx ON interview_sessions (retry_of_session_id) WHERE retry_of_session_id IS NOT NULL;
SELECT attach_updated_at_trigger('interview_sessions');

CREATE TABLE interview_messages (
  id                   uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  interview_session_id uuid NOT NULL REFERENCES interview_sessions(id) ON DELETE CASCADE,
  sequence             integer NOT NULL CHECK (sequence >= 1),
  role                 text NOT NULL CHECK (role IN ('ai','user')),
  kind                 text NOT NULL CHECK (kind IN ('question','follow_up','answer')),
  question_id          uuid NOT NULL REFERENCES questions(id) ON DELETE RESTRICT,   -- pertanyaan utama induk rantai
  parent_message_id    uuid REFERENCES interview_messages(id) ON DELETE RESTRICT,
  depth                smallint NOT NULL DEFAULT 0 CHECK (depth BETWEEN 0 AND 10),
  content              text NOT NULL CHECK (char_length(content) BETWEEN 1 AND 10000),
  ai_task_id           uuid REFERENCES ai_tasks(id) ON DELETE SET NULL,
  -- Answer_Evaluation_Status hanya untuk pesan kind='answer' (Requirement 25.1). Pesan AI tidak memilikinya.
  evaluation_status             text CHECK (evaluation_status IN ('pending','scored','failed','unscored')),
  evaluation_retry_count        smallint NOT NULL DEFAULT 0 CHECK (evaluation_retry_count >= 0),
  evaluation_failed_at          timestamptz,           -- kegagalan pertama; tidak di-reset oleh retry (A-24)
  evaluation_prompt_contract_id uuid REFERENCES prompt_contracts(id) ON DELETE RESTRICT,   -- versi tersimpan pada sesi, dipakai ulang oleh retry
  created_at           timestamptz NOT NULL DEFAULT now(),
  CONSTRAINT interview_messages_eval_chk CHECK ((kind = 'answer') = (evaluation_status IS NOT NULL)),
  CONSTRAINT interview_messages_failed_chk CHECK (evaluation_status IS DISTINCT FROM 'failed' OR evaluation_failed_at IS NOT NULL),
  CONSTRAINT interview_messages_seq_uk UNIQUE (interview_session_id, sequence),
  CONSTRAINT interview_messages_role_kind_chk CHECK (
       (role = 'ai'   AND kind IN ('question','follow_up'))
    OR (role = 'user' AND kind = 'answer')),
  CONSTRAINT interview_messages_shape_chk CHECK (
       (kind = 'question'  AND parent_message_id IS NULL AND depth = 0)
    OR (kind = 'follow_up' AND parent_message_id IS NOT NULL AND depth >= 1)
    OR (kind = 'answer'    AND parent_message_id IS NOT NULL))
);
-- setiap pesan memiliki paling banyak satu anak: satu jawaban per pertanyaan, satu follow-up per jawaban
CREATE UNIQUE INDEX interview_messages_one_child_uk ON interview_messages (parent_message_id) WHERE parent_message_id IS NOT NULL;
CREATE INDEX interview_messages_question_idx ON interview_messages (interview_session_id, question_id);
CREATE INDEX interview_messages_failed_idx   ON interview_messages (evaluation_failed_at) WHERE evaluation_status = 'failed';
CREATE INDEX interview_messages_open_eval_idx ON interview_messages (interview_session_id) WHERE evaluation_status IN ('pending','failed');
-- Isi jawaban (content) tidak berubah oleh Evaluation_Retry (Requirement 25.4). Pesan tidak pernah di-UPDATE pada kolom content.
