CREATE TABLE assessment_attempts (
  id                       uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  user_id                  uuid NOT NULL REFERENCES users(id) ON DELETE RESTRICT,
  topic_id                 uuid NOT NULL REFERENCES topics(id) ON DELETE RESTRICT,
  locale                   text NOT NULL REFERENCES supported_locales(code),
  -- submitted = menunggu: ada jawaban pending atau failed (Requirement 20.12).
  -- scored    = sedikitnya satu jawaban scored, topic_attempts dibuat.
  -- unscored  = seluruh jawaban non-kosong unscored, topic_attempts tidak dibuat (Requirement 20.11).
  status                   text NOT NULL DEFAULT 'in_progress'
                             CHECK (status IN ('in_progress','submitted','scored','unscored')),
  score_pct                numeric(5,2) CHECK (score_pct BETWEEN 0 AND 100),
  has_application_question boolean NOT NULL DEFAULT false,
  submitted_at             timestamptz,
  scored_at                timestamptz,
  resolved_at              timestamptz,                -- waktu attempt menjadi scored atau unscored
  created_at               timestamptz NOT NULL DEFAULT now(),
  updated_at               timestamptz NOT NULL DEFAULT now(),
  CONSTRAINT assessment_attempts_scored_chk   CHECK (status <> 'scored' OR (score_pct IS NOT NULL AND scored_at IS NOT NULL)),
  CONSTRAINT assessment_attempts_unscored_chk CHECK (status <> 'unscored' OR (score_pct IS NULL AND scored_at IS NULL)),
  CONSTRAINT assessment_attempts_resolved_chk CHECK ((status IN ('scored','unscored')) = (resolved_at IS NOT NULL))
);
CREATE INDEX assessment_attempts_user_idx ON assessment_attempts (user_id, created_at DESC, id DESC);
CREATE INDEX assessment_attempts_topic_idx ON assessment_attempts (user_id, topic_id, status);
SELECT attach_updated_at_trigger('assessment_attempts');

CREATE TABLE assessment_answers (
  id           uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  attempt_id   uuid NOT NULL REFERENCES assessment_attempts(id) ON DELETE CASCADE,
  question_id  uuid NOT NULL REFERENCES questions(id) ON DELETE RESTRICT,
  position     smallint NOT NULL CHECK (position >= 1),
  answer_text  text CHECK (answer_text IS NULL OR char_length(answer_text) <= 8000),
  is_blank     boolean GENERATED ALWAYS AS (answer_text IS NULL OR btrim(answer_text) = '') STORED,
  answered_at  timestamptz,
  -- Answer_Evaluation_Status (Requirement 25.1). NULL selama attempt belum di-submit (jawaban masih draf).
  evaluation_status             text CHECK (evaluation_status IN ('pending','scored','failed','unscored')),
  evaluation_retry_count        smallint NOT NULL DEFAULT 0 CHECK (evaluation_retry_count >= 0),
  evaluation_failed_at          timestamptz,           -- kegagalan pertama; tidak di-reset retry (dasar aturan 24 jam, A-24)
  evaluation_prompt_contract_id uuid REFERENCES prompt_contracts(id) ON DELETE RESTRICT,   -- kontrak yang dipakai ulang oleh retry
  created_at   timestamptz NOT NULL DEFAULT now(),
  updated_at   timestamptz NOT NULL DEFAULT now(),
  CONSTRAINT assessment_answers_q_uk UNIQUE (attempt_id, question_id),
  CONSTRAINT assessment_answers_pos_uk UNIQUE (attempt_id, position),
  CONSTRAINT assessment_answers_failed_chk CHECK (evaluation_status <> 'failed' OR evaluation_failed_at IS NOT NULL)
);
-- Jawaban kosong langsung scored (skor 0 tanpa AI, Requirement 20.7). Isi answer_text tidak diubah oleh retry (Requirement 25.4);
-- aplikasi tidak menyediakan jalur UPDATE answer_text setelah evaluation_status terisi, dan trigger berikut menjaganya.
CREATE OR REPLACE FUNCTION answer_text_frozen_after_submit() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
  IF OLD.evaluation_status IS NOT NULL AND NEW.answer_text IS DISTINCT FROM OLD.answer_text THEN
    RAISE EXCEPTION 'answer_text_frozen' USING ERRCODE = '23514';
  END IF;
  RETURN NEW;
END $$;
CREATE TRIGGER trg_assessment_answers_frozen BEFORE UPDATE OF answer_text ON assessment_answers
  FOR EACH ROW EXECUTE FUNCTION answer_text_frozen_after_submit();
CREATE INDEX assessment_answers_failed_idx  ON assessment_answers (evaluation_failed_at) WHERE evaluation_status = 'failed';
CREATE INDEX assessment_answers_open_idx    ON assessment_answers (attempt_id) WHERE evaluation_status IN ('pending','failed');
SELECT attach_updated_at_trigger('assessment_answers');
