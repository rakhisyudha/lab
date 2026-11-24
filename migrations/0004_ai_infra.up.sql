-- Registry Task_Type (Requirement 15.4): task baru = INSERT baris, tanpa ALTER TABLE
CREATE TABLE ai_task_types (
  task_type        text PRIMARY KEY CHECK (task_type ~ '^[a-z][a-z0-9_]*$'),
  description      text NOT NULL,
  default_priority smallint NOT NULL CHECK (default_priority BETWEEN 1 AND 3),  -- 1 tertinggi
  is_enabled       boolean NOT NULL DEFAULT true,
  created_at       timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE prompt_contracts (
  id                       uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  task_type                text NOT NULL REFERENCES ai_task_types(task_type) ON DELETE RESTRICT,
  version                  integer NOT NULL CHECK (version >= 1),
  contract_id              text GENERATED ALWAYS AS (task_type || '@v' || version::text) STORED,
  locale_policy            text NOT NULL DEFAULT 'session_locale'
                             CHECK (locale_policy IN ('session_locale','fixed_locale')),
  system_prompt            text NOT NULL,
  user_prompt_template     text NOT NULL,
  input_schema             jsonb NOT NULL,
  output_schema            jsonb NOT NULL,
  parameters               jsonb NOT NULL,              -- temperature, max_output_tokens, timeout_seconds, max_validation_retries
  status                   text NOT NULL DEFAULT 'draft'
                             CHECK (status IN ('draft','candidate','active','retired')),
  content_sha256           text NOT NULL,               -- hash kanonik seluruh isi kontrak
  activated_golden_run_id  uuid,                        -- FK ditambah di 0008
  activation_waiver_reason text,
  created_by               uuid REFERENCES users(id) ON DELETE SET NULL,
  activated_at             timestamptz,
  created_at               timestamptz NOT NULL DEFAULT now(),
  updated_at               timestamptz NOT NULL DEFAULT now(),
  CONSTRAINT prompt_contracts_ver_uk UNIQUE (task_type, version),
  CONSTRAINT prompt_contracts_params_chk CHECK (jsonb_typeof(parameters) = 'object'),
  CONSTRAINT prompt_contracts_active_chk CHECK (status <> 'active' OR activated_at IS NOT NULL)
);
CREATE UNIQUE INDEX prompt_contracts_one_active_uk ON prompt_contracts (task_type) WHERE status = 'active';
SELECT attach_updated_at_trigger('prompt_contracts');

-- Immutability (Requirement 15.11): isi tidak boleh berubah setelah keluar dari draft
CREATE OR REPLACE FUNCTION prompt_contracts_immutable() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
  IF OLD.status <> 'draft' AND (
       NEW.system_prompt <> OLD.system_prompt OR NEW.user_prompt_template <> OLD.user_prompt_template
    OR NEW.input_schema <> OLD.input_schema   OR NEW.output_schema <> OLD.output_schema
    OR NEW.parameters <> OLD.parameters       OR NEW.content_sha256 <> OLD.content_sha256) THEN
    RAISE EXCEPTION 'prompt_contract_immutable' USING ERRCODE = '23514';
  END IF;
  RETURN NEW;
END $$;
CREATE TRIGGER trg_prompt_contracts_immutable BEFORE UPDATE ON prompt_contracts
  FOR EACH ROW EXECUTE FUNCTION prompt_contracts_immutable();

CREATE TABLE ai_provider_settings (
  id            uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  provider_name text NOT NULL,                          -- kunci API hanya dari env/secret store
  model_name    text NOT NULL,
  fallback_order smallint NOT NULL CHECK (fallback_order >= 1),   -- 1 = primary
  rpm_limit     integer NOT NULL CHECK (rpm_limit > 0),
  tpm_limit     integer NOT NULL CHECK (tpm_limit > 0),
  is_enabled    boolean NOT NULL DEFAULT true,
  created_at    timestamptz NOT NULL DEFAULT now(),
  updated_at    timestamptz NOT NULL DEFAULT now(),
  CONSTRAINT ai_provider_settings_uk UNIQUE (provider_name, model_name)
);
CREATE UNIQUE INDEX ai_provider_settings_order_uk ON ai_provider_settings (fallback_order) WHERE is_enabled;
SELECT attach_updated_at_trigger('ai_provider_settings');

CREATE TABLE ai_tasks (
  id                     uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  user_id                uuid REFERENCES users(id) ON DELETE SET NULL,   -- null = task sistem (golden run)
  task_type              text NOT NULL REFERENCES ai_task_types(task_type) ON DELETE RESTRICT,
  priority               smallint NOT NULL CHECK (priority BETWEEN 1 AND 3),
  status                 text NOT NULL DEFAULT 'queued'
                           CHECK (status IN ('queued','processing','completed','failed')),
  payload                jsonb NOT NULL,
  result                 jsonb,
  error_code             text,
  error_message          text,
  attempts               integer NOT NULL DEFAULT 0 CHECK (attempts >= 0),
  max_attempts           integer NOT NULL DEFAULT 5 CHECK (max_attempts >= 1),
  run_after              timestamptz NOT NULL DEFAULT now(),
  locked_at              timestamptz,
  locked_by              text,
  started_at             timestamptz,
  finished_at            timestamptz,
  estimated_tokens       integer NOT NULL DEFAULT 0 CHECK (estimated_tokens >= 0),
  provider_name          text,
  fallback_used          boolean NOT NULL DEFAULT false,
  prompt_contract_id     uuid REFERENCES prompt_contracts(id) ON DELETE RESTRICT,
  resource_type          text,                                            -- assessment_answer | interview_message | interview_session | golden_case
  resource_id            uuid,
  idempotency_key        text,
  request_id             text,
  created_at             timestamptz NOT NULL DEFAULT now(),
  updated_at             timestamptz NOT NULL DEFAULT now(),
  CONSTRAINT ai_tasks_lock_chk      CHECK ((status = 'processing') = (locked_at IS NOT NULL)),
  CONSTRAINT ai_tasks_completed_chk CHECK (status <> 'completed' OR result IS NOT NULL),
  CONSTRAINT ai_tasks_failed_chk    CHECK (status <> 'failed' OR error_code IS NOT NULL)
);
-- dequeue: prioritas, lalu run_after, lalu FIFO
CREATE INDEX ai_tasks_dequeue_idx ON ai_tasks (priority, run_after, created_at) WHERE status = 'queued';
CREATE INDEX ai_tasks_user_active_idx ON ai_tasks (user_id) WHERE status IN ('queued','processing');
CREATE INDEX ai_tasks_reaper_idx ON ai_tasks (locked_at) WHERE status = 'processing';
CREATE INDEX ai_tasks_resource_idx ON ai_tasks (resource_type, resource_id);
CREATE UNIQUE INDEX ai_tasks_idem_uk ON ai_tasks (user_id, idempotency_key) WHERE idempotency_key IS NOT NULL;
SELECT attach_updated_at_trigger('ai_tasks');

CREATE TABLE ai_usage_logs (
  id                      uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  ai_task_id              uuid REFERENCES ai_tasks(id) ON DELETE SET NULL,
  user_id                 uuid REFERENCES users(id) ON DELETE SET NULL,
  task_type               text NOT NULL REFERENCES ai_task_types(task_type),
  prompt_contract_version integer,
  provider_name           text NOT NULL,
  model_name              text NOT NULL,
  attempt_no              smallint NOT NULL DEFAULT 1,
  input_tokens            integer NOT NULL DEFAULT 0 CHECK (input_tokens >= 0),
  output_tokens           integer NOT NULL DEFAULT 0 CHECK (output_tokens >= 0),
  latency_ms              integer NOT NULL DEFAULT 0 CHECK (latency_ms >= 0),
  status                  text NOT NULL CHECK (status IN
                             ('success','timeout','rate_limited','provider_error','validation_failed')),
  http_status             smallint,
  request_id              text,
  created_at              timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX ai_usage_user_time_idx ON ai_usage_logs (user_id, created_at);
CREATE INDEX ai_usage_time_idx      ON ai_usage_logs (created_at);
CREATE INDEX ai_usage_task_idx      ON ai_usage_logs (ai_task_id);

-- Kuota AI tidak memiliki tabel sendiri. Kuota disimpan pada app_settings dengan kunci
-- ai.quota.{scope}.{period}.{metric}, scope in (global, user), period in (daily, monthly), metric in (requests, total_tokens).
-- value = batas (integer >= 1) atau null (tanpa batas). Override kuota per user di luar P0 (lihat DA-16).

CREATE TABLE ai_injection_events (
  id          uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  user_id     uuid REFERENCES users(id) ON DELETE SET NULL,
  ai_task_id  uuid REFERENCES ai_tasks(id) ON DELETE SET NULL,
  source_type text NOT NULL,                              -- assessment_answer | interview_message | golden_case
  source_id   uuid,
  detector    text NOT NULL CHECK (detector IN ('input_heuristic','model_flag','output_violation')),
  pattern     text NOT NULL,                              -- nama rule heuristik atau jenis pelanggaran
  excerpt     text NOT NULL DEFAULT '' CHECK (char_length(excerpt) <= 300),
  request_id  text,
  created_at  timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX ai_injection_events_user_idx ON ai_injection_events (user_id, created_at DESC);

-- Parameter scoring, mastery, AI, dan token yang dapat diubah Admin tanpa deploy (Requirement 24, D-10).
-- is_initial_value = true berarti nilai masih Initial_Value dari seed, bukan nilai final produk.
CREATE TABLE app_settings (
  key              text PRIMARY KEY CHECK (key ~ '^[a-z][a-z0-9_.]*$'),
  value            jsonb NOT NULL,
  value_schema     jsonb NOT NULL,                      -- JSON Schema tipe dan rentang (Requirement 24.6)
  description      text NOT NULL DEFAULT '',
  is_initial_value boolean NOT NULL DEFAULT true,
  updated_by       uuid REFERENCES users(id) ON DELETE SET NULL,
  created_at       timestamptz NOT NULL DEFAULT now(),
  updated_at       timestamptz NOT NULL DEFAULT now(),
  CONSTRAINT app_settings_schema_chk CHECK (jsonb_typeof(value_schema) = 'object')
);
SELECT attach_updated_at_trigger('app_settings');

-- Pengaman tingkat database (Requirement 24.3): perubahan nilai selalu menurunkan is_initial_value.
-- API juga mengisi updated_by dan menetapkan is_initial_value = false secara eksplisit.
CREATE OR REPLACE FUNCTION app_settings_mark_changed() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
  IF NEW.value IS DISTINCT FROM OLD.value THEN NEW.is_initial_value := false; END IF;
  RETURN NEW;
END $$;
CREATE TRIGGER trg_app_settings_mark_changed BEFORE UPDATE OF value ON app_settings
  FOR EACH ROW EXECUTE FUNCTION app_settings_mark_changed();

-- Penyimpanan state token bucket (Requirement 16.17). Dipakai bila RATELIMIT_STORE=postgres, yaitu default
-- dan wajib pada deployment multi-instance (D-15). Kunci: 'http:{kelas}:{ip|user|email}' dan 'provider:{nama}:{rpm|tpm}'.
-- Mode memori (single-instance atau dev) tidak memakai tabel ini.
CREATE TABLE rate_buckets (
  key        text PRIMARY KEY,
  tokens     double precision NOT NULL CHECK (tokens >= 0),
  updated_at timestamptz NOT NULL
);
CREATE INDEX rate_buckets_updated_idx ON rate_buckets (updated_at);   -- job pembersihan bucket penuh yang lama tidak aktif
