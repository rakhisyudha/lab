--
-- PostgreSQL database dump
--



SET statement_timeout = 0;
SET lock_timeout = 0;
SET idle_in_transaction_session_timeout = 0;
SET transaction_timeout = 0;
SET client_encoding = 'UTF8';
SET standard_conforming_strings = on;
SELECT pg_catalog.set_config('search_path', '', false);
SET check_function_bodies = false;
SET xmloption = content;
SET client_min_messages = warning;
SET row_security = off;

--
-- Name: answer_text_frozen_after_submit(); Type: FUNCTION; Schema: public; Owner: -
--

CREATE FUNCTION public.answer_text_frozen_after_submit() RETURNS trigger
    LANGUAGE plpgsql
    AS $$
BEGIN
  IF OLD.evaluation_status IS NOT NULL AND NEW.answer_text IS DISTINCT FROM OLD.answer_text THEN
    RAISE EXCEPTION 'answer_text_frozen' USING ERRCODE = '23514';
  END IF;
  RETURN NEW;
END $$;


--
-- Name: app_settings_mark_changed(); Type: FUNCTION; Schema: public; Owner: -
--

CREATE FUNCTION public.app_settings_mark_changed() RETURNS trigger
    LANGUAGE plpgsql
    AS $$
BEGIN
  IF NEW.value IS DISTINCT FROM OLD.value THEN NEW.is_initial_value := false; END IF;
  RETURN NEW;
END $$;


--
-- Name: attach_translation_triggers(regclass); Type: FUNCTION; Schema: public; Owner: -
--

CREATE FUNCTION public.attach_translation_triggers(tbl regclass) RETURNS void
    LANGUAGE plpgsql
    AS $$
BEGIN
  PERFORM attach_updated_at_trigger(tbl);
  EXECUTE format(
    'CREATE TRIGGER trg_reviewer_admin BEFORE INSERT OR UPDATE OF reviewer_id ON %s
       FOR EACH ROW EXECUTE FUNCTION enforce_reviewer_admin()', tbl);
END $$;


--
-- Name: attach_updated_at_trigger(regclass); Type: FUNCTION; Schema: public; Owner: -
--

CREATE FUNCTION public.attach_updated_at_trigger(tbl regclass) RETURNS void
    LANGUAGE plpgsql
    AS $$
BEGIN
  EXECUTE format(
    'CREATE TRIGGER trg_set_updated_at BEFORE UPDATE ON %s
       FOR EACH ROW EXECUTE FUNCTION set_updated_at()', tbl);
END $$;


--
-- Name: enforce_reviewer_admin(); Type: FUNCTION; Schema: public; Owner: -
--

CREATE FUNCTION public.enforce_reviewer_admin() RETURNS trigger
    LANGUAGE plpgsql
    AS $$
BEGIN
  IF NEW.reviewer_id IS NOT NULL AND NOT EXISTS (
       SELECT 1 FROM users u WHERE u.id = NEW.reviewer_id AND u.role = 'admin') THEN
    RAISE EXCEPTION 'reviewer_must_be_admin' USING ERRCODE = '23514';
  END IF;
  RETURN NEW;
END $$;


--
-- Name: misconceptions_immutable(); Type: FUNCTION; Schema: public; Owner: -
--

CREATE FUNCTION public.misconceptions_immutable() RETURNS trigger
    LANGUAGE plpgsql
    AS $$
BEGIN
  IF NEW.code <> OLD.code OR NEW.topic_id <> OLD.topic_id THEN
    RAISE EXCEPTION 'misconception_code_immutable' USING ERRCODE = '23514';
  END IF;
  RETURN NEW;
END $$;


--
-- Name: prompt_contracts_immutable(); Type: FUNCTION; Schema: public; Owner: -
--

CREATE FUNCTION public.prompt_contracts_immutable() RETURNS trigger
    LANGUAGE plpgsql
    AS $$
BEGIN
  IF OLD.status <> 'draft' AND (
       NEW.system_prompt <> OLD.system_prompt OR NEW.user_prompt_template <> OLD.user_prompt_template
    OR NEW.input_schema <> OLD.input_schema   OR NEW.output_schema <> OLD.output_schema
    OR NEW.parameters <> OLD.parameters       OR NEW.content_sha256 <> OLD.content_sha256) THEN
    RAISE EXCEPTION 'prompt_contract_immutable' USING ERRCODE = '23514';
  END IF;
  RETURN NEW;
END $$;


--
-- Name: set_updated_at(); Type: FUNCTION; Schema: public; Owner: -
--

CREATE FUNCTION public.set_updated_at() RETURNS trigger
    LANGUAGE plpgsql
    AS $$
BEGIN
  NEW.updated_at := now();
  RETURN NEW;
END $$;


SET default_tablespace = '';

SET default_table_access_method = heap;

--
-- Name: ai_injection_events; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.ai_injection_events (
    id uuid DEFAULT gen_random_uuid() NOT NULL,
    user_id uuid,
    ai_task_id uuid,
    source_type text NOT NULL,
    source_id uuid,
    detector text NOT NULL,
    pattern text NOT NULL,
    excerpt text DEFAULT ''::text NOT NULL,
    request_id text,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    CONSTRAINT ai_injection_events_detector_check CHECK ((detector = ANY (ARRAY['input_heuristic'::text, 'model_flag'::text, 'output_violation'::text]))),
    CONSTRAINT ai_injection_events_excerpt_check CHECK ((char_length(excerpt) <= 300))
);


--
-- Name: ai_provider_settings; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.ai_provider_settings (
    id uuid DEFAULT gen_random_uuid() NOT NULL,
    provider_name text NOT NULL,
    model_name text NOT NULL,
    fallback_order smallint NOT NULL,
    rpm_limit integer NOT NULL,
    tpm_limit integer NOT NULL,
    is_enabled boolean DEFAULT true NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL,
    CONSTRAINT ai_provider_settings_fallback_order_check CHECK ((fallback_order >= 1)),
    CONSTRAINT ai_provider_settings_rpm_limit_check CHECK ((rpm_limit > 0)),
    CONSTRAINT ai_provider_settings_tpm_limit_check CHECK ((tpm_limit > 0))
);


--
-- Name: ai_task_types; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.ai_task_types (
    task_type text NOT NULL,
    description text NOT NULL,
    default_priority smallint NOT NULL,
    is_enabled boolean DEFAULT true NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    CONSTRAINT ai_task_types_default_priority_check CHECK (((default_priority >= 1) AND (default_priority <= 3))),
    CONSTRAINT ai_task_types_task_type_check CHECK ((task_type ~ '^[a-z][a-z0-9_]*$'::text))
);


--
-- Name: ai_tasks; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.ai_tasks (
    id uuid DEFAULT gen_random_uuid() NOT NULL,
    user_id uuid,
    task_type text NOT NULL,
    priority smallint NOT NULL,
    status text DEFAULT 'queued'::text NOT NULL,
    payload jsonb NOT NULL,
    result jsonb,
    error_code text,
    error_message text,
    attempts integer DEFAULT 0 NOT NULL,
    max_attempts integer DEFAULT 5 NOT NULL,
    run_after timestamp with time zone DEFAULT now() NOT NULL,
    locked_at timestamp with time zone,
    locked_by text,
    started_at timestamp with time zone,
    finished_at timestamp with time zone,
    estimated_tokens integer DEFAULT 0 NOT NULL,
    provider_name text,
    fallback_used boolean DEFAULT false NOT NULL,
    prompt_contract_id uuid,
    resource_type text,
    resource_id uuid,
    idempotency_key text,
    request_id text,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL,
    CONSTRAINT ai_tasks_attempts_check CHECK ((attempts >= 0)),
    CONSTRAINT ai_tasks_completed_chk CHECK (((status <> 'completed'::text) OR (result IS NOT NULL))),
    CONSTRAINT ai_tasks_estimated_tokens_check CHECK ((estimated_tokens >= 0)),
    CONSTRAINT ai_tasks_failed_chk CHECK (((status <> 'failed'::text) OR (error_code IS NOT NULL))),
    CONSTRAINT ai_tasks_lock_chk CHECK (((status = 'processing'::text) = (locked_at IS NOT NULL))),
    CONSTRAINT ai_tasks_max_attempts_check CHECK ((max_attempts >= 1)),
    CONSTRAINT ai_tasks_priority_check CHECK (((priority >= 1) AND (priority <= 3))),
    CONSTRAINT ai_tasks_status_check CHECK ((status = ANY (ARRAY['queued'::text, 'processing'::text, 'completed'::text, 'failed'::text])))
);


--
-- Name: ai_usage_logs; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.ai_usage_logs (
    id uuid DEFAULT gen_random_uuid() NOT NULL,
    ai_task_id uuid,
    user_id uuid,
    task_type text NOT NULL,
    prompt_contract_version integer,
    provider_name text NOT NULL,
    model_name text NOT NULL,
    attempt_no smallint DEFAULT 1 NOT NULL,
    input_tokens integer DEFAULT 0 NOT NULL,
    output_tokens integer DEFAULT 0 NOT NULL,
    latency_ms integer DEFAULT 0 NOT NULL,
    status text NOT NULL,
    http_status smallint,
    request_id text,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    CONSTRAINT ai_usage_logs_input_tokens_check CHECK ((input_tokens >= 0)),
    CONSTRAINT ai_usage_logs_latency_ms_check CHECK ((latency_ms >= 0)),
    CONSTRAINT ai_usage_logs_output_tokens_check CHECK ((output_tokens >= 0)),
    CONSTRAINT ai_usage_logs_status_check CHECK ((status = ANY (ARRAY['success'::text, 'timeout'::text, 'rate_limited'::text, 'provider_error'::text, 'validation_failed'::text])))
);


--
-- Name: answer_evaluation_misconceptions; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.answer_evaluation_misconceptions (
    answer_evaluation_id uuid NOT NULL,
    misconception_id uuid NOT NULL,
    kind text NOT NULL,
    CONSTRAINT answer_evaluation_misconceptions_kind_check CHECK ((kind = ANY (ARRAY['detected'::text, 'resolved'::text])))
);


--
-- Name: answer_evaluations; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.answer_evaluations (
    id uuid DEFAULT gen_random_uuid() NOT NULL,
    user_id uuid NOT NULL,
    topic_id uuid NOT NULL,
    question_id uuid NOT NULL,
    locale text NOT NULL,
    assessment_answer_id uuid,
    interview_message_id uuid,
    ai_task_id uuid,
    prompt_contract_id uuid,
    model_name text,
    kind text DEFAULT 'ai'::text NOT NULL,
    relevance text,
    rubric_score numeric(5,2),
    score_pct numeric(5,2),
    score_hint numeric(5,2),
    concept_results jsonb DEFAULT '[]'::jsonb NOT NULL,
    penalties jsonb DEFAULT '{}'::jsonb NOT NULL,
    feedback text DEFAULT ''::text NOT NULL,
    confidence numeric(3,2),
    needs_review boolean DEFAULT false NOT NULL,
    injection_suspected boolean DEFAULT false NOT NULL,
    validation_attempts smallint DEFAULT 1 NOT NULL,
    params_snapshot jsonb DEFAULT '{}'::jsonb NOT NULL,
    raw_output jsonb,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    CONSTRAINT answer_evaluations_blank_chk CHECK (((kind = 'ai'::text) OR ((relevance = 'empty'::text) AND (prompt_contract_id IS NULL)))),
    CONSTRAINT answer_evaluations_confidence_check CHECK (((confidence >= (0)::numeric) AND (confidence <= (1)::numeric))),
    CONSTRAINT answer_evaluations_kind_check CHECK ((kind = ANY (ARRAY['ai'::text, 'blank'::text]))),
    CONSTRAINT answer_evaluations_relevance_check CHECK ((relevance = ANY (ARRAY['on_topic'::text, 'off_topic'::text, 'empty'::text]))),
    CONSTRAINT answer_evaluations_rubric_score_check CHECK (((rubric_score >= (0)::numeric) AND (rubric_score <= (100)::numeric))),
    CONSTRAINT answer_evaluations_score_chk CHECK ((score_pct IS NOT NULL)),
    CONSTRAINT answer_evaluations_score_pct_check CHECK (((score_pct >= (0)::numeric) AND (score_pct <= (100)::numeric))),
    CONSTRAINT answer_evaluations_source_chk CHECK ((num_nonnulls(assessment_answer_id, interview_message_id) = 1))
);


--
-- Name: app_settings; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.app_settings (
    key text NOT NULL,
    value jsonb NOT NULL,
    value_schema jsonb NOT NULL,
    description text DEFAULT ''::text NOT NULL,
    is_initial_value boolean DEFAULT true NOT NULL,
    updated_by uuid,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL,
    CONSTRAINT app_settings_key_check CHECK ((key ~ '^[a-z][a-z0-9_.]*$'::text)),
    CONSTRAINT app_settings_schema_chk CHECK ((jsonb_typeof(value_schema) = 'object'::text))
);


--
-- Name: assessment_answers; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.assessment_answers (
    id uuid DEFAULT gen_random_uuid() NOT NULL,
    attempt_id uuid NOT NULL,
    question_id uuid NOT NULL,
    "position" smallint NOT NULL,
    answer_text text,
    is_blank boolean GENERATED ALWAYS AS (((answer_text IS NULL) OR (btrim(answer_text) = ''::text))) STORED,
    answered_at timestamp with time zone,
    evaluation_status text,
    evaluation_retry_count smallint DEFAULT 0 NOT NULL,
    evaluation_failed_at timestamp with time zone,
    evaluation_prompt_contract_id uuid,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL,
    CONSTRAINT assessment_answers_answer_text_check CHECK (((answer_text IS NULL) OR (char_length(answer_text) <= 8000))),
    CONSTRAINT assessment_answers_evaluation_retry_count_check CHECK ((evaluation_retry_count >= 0)),
    CONSTRAINT assessment_answers_evaluation_status_check CHECK ((evaluation_status = ANY (ARRAY['pending'::text, 'scored'::text, 'failed'::text, 'unscored'::text]))),
    CONSTRAINT assessment_answers_failed_chk CHECK (((evaluation_status <> 'failed'::text) OR (evaluation_failed_at IS NOT NULL))),
    CONSTRAINT assessment_answers_position_check CHECK (("position" >= 1))
);


--
-- Name: assessment_attempts; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.assessment_attempts (
    id uuid DEFAULT gen_random_uuid() NOT NULL,
    user_id uuid NOT NULL,
    topic_id uuid NOT NULL,
    locale text NOT NULL,
    status text DEFAULT 'in_progress'::text NOT NULL,
    score_pct numeric(5,2),
    has_application_question boolean DEFAULT false NOT NULL,
    submitted_at timestamp with time zone,
    scored_at timestamp with time zone,
    resolved_at timestamp with time zone,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL,
    CONSTRAINT assessment_attempts_resolved_chk CHECK (((status = ANY (ARRAY['scored'::text, 'unscored'::text])) = (resolved_at IS NOT NULL))),
    CONSTRAINT assessment_attempts_score_pct_check CHECK (((score_pct >= (0)::numeric) AND (score_pct <= (100)::numeric))),
    CONSTRAINT assessment_attempts_scored_chk CHECK (((status <> 'scored'::text) OR ((score_pct IS NOT NULL) AND (scored_at IS NOT NULL)))),
    CONSTRAINT assessment_attempts_status_check CHECK ((status = ANY (ARRAY['in_progress'::text, 'submitted'::text, 'scored'::text, 'unscored'::text]))),
    CONSTRAINT assessment_attempts_unscored_chk CHECK (((status <> 'unscored'::text) OR ((score_pct IS NULL) AND (scored_at IS NULL))))
);


--
-- Name: concept_translations; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.concept_translations (
    id uuid DEFAULT gen_random_uuid() NOT NULL,
    concept_id uuid NOT NULL,
    locale text NOT NULL,
    name text NOT NULL,
    description text DEFAULT ''::text NOT NULL,
    status text DEFAULT 'draft'::text NOT NULL,
    version integer DEFAULT 1 NOT NULL,
    reviewer_id uuid,
    reviewed_at timestamp with time zone,
    published_at timestamp with time zone,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL,
    CONSTRAINT concept_translations_name_check CHECK (((char_length(name) >= 1) AND (char_length(name) <= 200))),
    CONSTRAINT concept_translations_pub_chk CHECK (((status <> 'published'::text) OR ((reviewer_id IS NOT NULL) AND (reviewed_at IS NOT NULL) AND (published_at IS NOT NULL)))),
    CONSTRAINT concept_translations_status_check CHECK ((status = ANY (ARRAY['draft'::text, 'in_review'::text, 'published'::text, 'archived'::text]))),
    CONSTRAINT concept_translations_version_check CHECK ((version >= 1))
);


--
-- Name: concepts; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.concepts (
    id uuid DEFAULT gen_random_uuid() NOT NULL,
    topic_id uuid NOT NULL,
    code text NOT NULL,
    is_active boolean DEFAULT true NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL,
    CONSTRAINT concepts_code_check CHECK ((code ~ '^[a-z][a-z0-9_]*$'::text))
);


--
-- Name: golden_cases; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.golden_cases (
    id uuid DEFAULT gen_random_uuid() NOT NULL,
    question_id uuid NOT NULL,
    locale text NOT NULL,
    category text NOT NULL,
    sample_answer text NOT NULL,
    expected_score_min numeric(5,2) NOT NULL,
    expected_score_max numeric(5,2) NOT NULL,
    expected_found_concept_codes text[] DEFAULT '{}'::text[] NOT NULL,
    expected_missed_concept_codes text[] DEFAULT '{}'::text[] NOT NULL,
    expected_misconception_codes text[] DEFAULT '{}'::text[] NOT NULL,
    notes text DEFAULT ''::text NOT NULL,
    owner_id uuid NOT NULL,
    is_active boolean DEFAULT true NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL,
    CONSTRAINT golden_cases_category_check CHECK ((category = ANY (ARRAY['different_wording'::text, 'partially_correct'::text, 'critical_misconception'::text, 'off_topic_or_empty'::text, 'adversarial_injection'::text]))),
    CONSTRAINT golden_cases_disjoint_chk CHECK ((NOT (expected_found_concept_codes && expected_missed_concept_codes))),
    CONSTRAINT golden_cases_expected_score_max_check CHECK (((expected_score_max >= (0)::numeric) AND (expected_score_max <= (100)::numeric))),
    CONSTRAINT golden_cases_expected_score_min_check CHECK (((expected_score_min >= (0)::numeric) AND (expected_score_min <= (100)::numeric))),
    CONSTRAINT golden_cases_range_chk CHECK ((expected_score_min <= expected_score_max)),
    CONSTRAINT golden_cases_sample_answer_check CHECK ((char_length(sample_answer) <= 8000))
);


--
-- Name: golden_recordings; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.golden_recordings (
    id uuid DEFAULT gen_random_uuid() NOT NULL,
    prompt_contract_id uuid NOT NULL,
    model_name text NOT NULL,
    golden_case_id uuid NOT NULL,
    case_sha256 text NOT NULL,
    responses jsonb NOT NULL,
    source_run_id uuid NOT NULL,
    recorded_at timestamp with time zone DEFAULT now() NOT NULL,
    CONSTRAINT golden_recordings_resp_chk CHECK (((jsonb_typeof(responses) = 'array'::text) AND (jsonb_array_length(responses) >= 1)))
);


--
-- Name: golden_run_results; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.golden_run_results (
    id uuid DEFAULT gen_random_uuid() NOT NULL,
    golden_run_id uuid NOT NULL,
    golden_case_id uuid NOT NULL,
    schema_valid boolean NOT NULL,
    catalog_violation boolean DEFAULT false NOT NULL,
    actual_score numeric(5,2),
    actual_found_codes text[] DEFAULT '{}'::text[] NOT NULL,
    actual_missed_codes text[] DEFAULT '{}'::text[] NOT NULL,
    actual_misconception_codes text[] DEFAULT '{}'::text[] NOT NULL,
    injection_obeyed boolean DEFAULT false NOT NULL,
    checks jsonb DEFAULT '{}'::jsonb NOT NULL,
    passed boolean NOT NULL,
    raw_output jsonb,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    CONSTRAINT golden_run_results_actual_score_check CHECK (((actual_score >= (0)::numeric) AND (actual_score <= (100)::numeric)))
);


--
-- Name: golden_runs; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.golden_runs (
    id uuid DEFAULT gen_random_uuid() NOT NULL,
    prompt_contract_id uuid NOT NULL,
    run_mode text NOT NULL,
    baseline_run_id uuid,
    model_name text NOT NULL,
    status text DEFAULT 'running'::text NOT NULL,
    error_reasons text[] DEFAULT '{}'::text[] NOT NULL,
    trigger_source text DEFAULT 'manual'::text NOT NULL,
    metrics jsonb DEFAULT '{}'::jsonb NOT NULL,
    thresholds jsonb DEFAULT '{}'::jsonb NOT NULL,
    coverage jsonb DEFAULT '{}'::jsonb NOT NULL,
    regression jsonb DEFAULT '{}'::jsonb NOT NULL,
    triggered_by uuid,
    started_at timestamp with time zone DEFAULT now() NOT NULL,
    finished_at timestamp with time zone,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL,
    CONSTRAINT golden_runs_run_mode_check CHECK ((run_mode = ANY (ARRAY['recorded'::text, 'live'::text]))),
    CONSTRAINT golden_runs_status_check CHECK ((status = ANY (ARRAY['running'::text, 'passed'::text, 'failed'::text, 'insufficient_coverage'::text, 'errored'::text]))),
    CONSTRAINT golden_runs_trigger_source_check CHECK ((trigger_source = ANY (ARRAY['manual'::text, 'schedule'::text, 'ci'::text])))
);


--
-- Name: interview_evaluations; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.interview_evaluations (
    id uuid DEFAULT gen_random_uuid() NOT NULL,
    interview_session_id uuid NOT NULL,
    status text NOT NULL,
    dimension_scores jsonb,
    weights jsonb,
    overall_score numeric(5,2),
    summary text DEFAULT ''::text NOT NULL,
    strengths jsonb DEFAULT '[]'::jsonb NOT NULL,
    improvements jsonb DEFAULT '[]'::jsonb NOT NULL,
    needs_review boolean DEFAULT false NOT NULL,
    prompt_contract_id uuid,
    model_name text,
    ai_task_id uuid,
    raw_output jsonb,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    CONSTRAINT interview_evaluations_ok_chk CHECK (((status <> 'succeeded'::text) OR ((dimension_scores IS NOT NULL) AND (overall_score IS NOT NULL)))),
    CONSTRAINT interview_evaluations_overall_score_check CHECK (((overall_score >= (0)::numeric) AND (overall_score <= (100)::numeric))),
    CONSTRAINT interview_evaluations_status_check CHECK ((status = ANY (ARRAY['succeeded'::text, 'failed'::text])))
);


--
-- Name: interview_messages; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.interview_messages (
    id uuid DEFAULT gen_random_uuid() NOT NULL,
    interview_session_id uuid NOT NULL,
    sequence integer NOT NULL,
    role text NOT NULL,
    kind text NOT NULL,
    question_id uuid NOT NULL,
    parent_message_id uuid,
    depth smallint DEFAULT 0 NOT NULL,
    content text NOT NULL,
    ai_task_id uuid,
    evaluation_status text,
    evaluation_retry_count smallint DEFAULT 0 NOT NULL,
    evaluation_failed_at timestamp with time zone,
    evaluation_prompt_contract_id uuid,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    CONSTRAINT interview_messages_content_check CHECK (((char_length(content) >= 1) AND (char_length(content) <= 10000))),
    CONSTRAINT interview_messages_depth_check CHECK (((depth >= 0) AND (depth <= 10))),
    CONSTRAINT interview_messages_eval_chk CHECK (((kind = 'answer'::text) = (evaluation_status IS NOT NULL))),
    CONSTRAINT interview_messages_evaluation_retry_count_check CHECK ((evaluation_retry_count >= 0)),
    CONSTRAINT interview_messages_evaluation_status_check CHECK ((evaluation_status = ANY (ARRAY['pending'::text, 'scored'::text, 'failed'::text, 'unscored'::text]))),
    CONSTRAINT interview_messages_failed_chk CHECK (((evaluation_status IS DISTINCT FROM 'failed'::text) OR (evaluation_failed_at IS NOT NULL))),
    CONSTRAINT interview_messages_kind_check CHECK ((kind = ANY (ARRAY['question'::text, 'follow_up'::text, 'answer'::text]))),
    CONSTRAINT interview_messages_role_check CHECK ((role = ANY (ARRAY['ai'::text, 'user'::text]))),
    CONSTRAINT interview_messages_role_kind_chk CHECK ((((role = 'ai'::text) AND (kind = ANY (ARRAY['question'::text, 'follow_up'::text]))) OR ((role = 'user'::text) AND (kind = 'answer'::text)))),
    CONSTRAINT interview_messages_sequence_check CHECK ((sequence >= 1)),
    CONSTRAINT interview_messages_shape_chk CHECK ((((kind = 'question'::text) AND (parent_message_id IS NULL) AND (depth = 0)) OR ((kind = 'follow_up'::text) AND (parent_message_id IS NOT NULL) AND (depth >= 1)) OR ((kind = 'answer'::text) AND (parent_message_id IS NOT NULL))))
);


--
-- Name: interview_profiles; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.interview_profiles (
    interview_type text NOT NULL,
    min_questions smallint NOT NULL,
    max_questions smallint NOT NULL,
    max_followups_per_question smallint NOT NULL,
    total_cap smallint NOT NULL,
    max_depth smallint NOT NULL,
    w_technical_accuracy smallint NOT NULL,
    w_conceptual_understanding smallint NOT NULL,
    w_reasoning smallint NOT NULL,
    w_practical_application smallint NOT NULL,
    w_communication smallint NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL,
    CONSTRAINT interview_profiles_cap_chk CHECK ((total_cap >= max_questions)),
    CONSTRAINT interview_profiles_interview_type_check CHECK ((interview_type = ANY (ARRAY['practice'::text, 'recruitment'::text]))),
    CONSTRAINT interview_profiles_max_depth_check CHECK ((max_depth >= 0)),
    CONSTRAINT interview_profiles_max_followups_per_question_check CHECK ((max_followups_per_question >= 0)),
    CONSTRAINT interview_profiles_min_questions_check CHECK ((min_questions >= 1)),
    CONSTRAINT interview_profiles_q_chk CHECK ((min_questions <= max_questions)),
    CONSTRAINT interview_profiles_w_chk CHECK ((((((w_technical_accuracy + w_conceptual_understanding) + w_reasoning) + w_practical_application) + w_communication) = 100)),
    CONSTRAINT interview_profiles_w_communication_check CHECK (((w_communication >= 0) AND (w_communication <= 100))),
    CONSTRAINT interview_profiles_w_conceptual_understanding_check CHECK (((w_conceptual_understanding >= 0) AND (w_conceptual_understanding <= 100))),
    CONSTRAINT interview_profiles_w_practical_application_check CHECK (((w_practical_application >= 0) AND (w_practical_application <= 100))),
    CONSTRAINT interview_profiles_w_reasoning_check CHECK (((w_reasoning >= 0) AND (w_reasoning <= 100))),
    CONSTRAINT interview_profiles_w_technical_accuracy_check CHECK (((w_technical_accuracy >= 0) AND (w_technical_accuracy <= 100)))
);


--
-- Name: interview_sessions; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.interview_sessions (
    id uuid DEFAULT gen_random_uuid() NOT NULL,
    user_id uuid NOT NULL,
    status text DEFAULT 'CREATED'::text NOT NULL,
    interview_type text NOT NULL,
    configuration jsonb NOT NULL,
    current_question_id uuid,
    current_question_index smallint DEFAULT 0 NOT NULL,
    session_state jsonb DEFAULT '{}'::jsonb NOT NULL,
    locale text NOT NULL,
    retry_of_session_id uuid,
    prompt_contract_versions jsonb DEFAULT '{}'::jsonb NOT NULL,
    started_at timestamp with time zone,
    paused_at timestamp with time zone,
    expires_at timestamp with time zone,
    completed_at timestamp with time zone,
    abandoned_at timestamp with time zone,
    retention_until timestamp with time zone,
    deleted_at timestamp with time zone,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL,
    CONSTRAINT interview_sessions_aband_chk CHECK (((status <> 'ABANDONED'::text) OR (abandoned_at IS NOT NULL))),
    CONSTRAINT interview_sessions_active_chk CHECK (((status <> ALL (ARRAY['CREATED'::text, 'IN_PROGRESS'::text])) OR ((paused_at IS NULL) AND (expires_at IS NULL)))),
    CONSTRAINT interview_sessions_cfg_chk CHECK (((jsonb_typeof(configuration) = 'object'::text) AND (jsonb_typeof(session_state) = 'object'::text))),
    CONSTRAINT interview_sessions_current_question_index_check CHECK ((current_question_index >= 0)),
    CONSTRAINT interview_sessions_done_chk CHECK (((status <> 'COMPLETED'::text) OR (completed_at IS NOT NULL))),
    CONSTRAINT interview_sessions_paused_chk CHECK (((status <> 'PAUSED'::text) OR ((paused_at IS NOT NULL) AND (expires_at IS NOT NULL)))),
    CONSTRAINT interview_sessions_started_chk CHECK (((status <> ALL (ARRAY['IN_PROGRESS'::text, 'PAUSED'::text, 'COMPLETED'::text])) OR (started_at IS NOT NULL))),
    CONSTRAINT interview_sessions_status_check CHECK ((status = ANY (ARRAY['CREATED'::text, 'IN_PROGRESS'::text, 'PAUSED'::text, 'COMPLETED'::text, 'ABANDONED'::text])))
);


--
-- Name: invitations; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.invitations (
    id uuid DEFAULT gen_random_uuid() NOT NULL,
    code_hash bytea NOT NULL,
    created_by uuid NOT NULL,
    email text,
    role text DEFAULT 'user'::text NOT NULL,
    expires_at timestamp with time zone NOT NULL,
    used_at timestamp with time zone,
    used_by uuid,
    revoked_at timestamp with time zone,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL,
    CONSTRAINT invitations_role_check CHECK ((role = ANY (ARRAY['user'::text, 'admin'::text]))),
    CONSTRAINT invitations_used_pair CHECK (((used_at IS NULL) = (used_by IS NULL)))
);


--
-- Name: mastery_records; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.mastery_records (
    id uuid DEFAULT gen_random_uuid() NOT NULL,
    user_id uuid NOT NULL,
    topic_id uuid NOT NULL,
    status text DEFAULT 'NOT_STARTED'::text NOT NULL,
    attempt_count integer DEFAULT 0 NOT NULL,
    avg_last3 numeric(5,2),
    best_score numeric(5,2),
    last_attempt_id uuid,
    mastered_at timestamp with time zone,
    interview_ready boolean,
    evidence jsonb DEFAULT '{}'::jsonb NOT NULL,
    rule_version text DEFAULT 'mastery_v1'::text NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL,
    CONSTRAINT mastery_records_attempt_count_check CHECK ((attempt_count >= 0)),
    CONSTRAINT mastery_records_avg_last3_check CHECK (((avg_last3 >= (0)::numeric) AND (avg_last3 <= (100)::numeric))),
    CONSTRAINT mastery_records_best_score_check CHECK (((best_score >= (0)::numeric) AND (best_score <= (100)::numeric))),
    CONSTRAINT mastery_records_mastered_chk CHECK (((status = 'MASTERED'::text) = (mastered_at IS NOT NULL))),
    CONSTRAINT mastery_records_status_check CHECK ((status = ANY (ARRAY['NOT_STARTED'::text, 'IN_PROGRESS'::text, 'PRACTICING'::text, 'NEEDS_REVIEW'::text, 'MASTERED'::text])))
);


--
-- Name: material_translations; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.material_translations (
    id uuid DEFAULT gen_random_uuid() NOT NULL,
    material_id uuid NOT NULL,
    locale text NOT NULL,
    title text NOT NULL,
    summary text DEFAULT ''::text NOT NULL,
    body_md text DEFAULT ''::text NOT NULL,
    status text DEFAULT 'draft'::text NOT NULL,
    version integer DEFAULT 1 NOT NULL,
    reviewer_id uuid,
    reviewed_at timestamp with time zone,
    published_at timestamp with time zone,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL,
    CONSTRAINT material_translations_pub_chk CHECK (((status <> 'published'::text) OR ((reviewer_id IS NOT NULL) AND (reviewed_at IS NOT NULL) AND (published_at IS NOT NULL)))),
    CONSTRAINT material_translations_status_check CHECK ((status = ANY (ARRAY['draft'::text, 'in_review'::text, 'published'::text, 'archived'::text]))),
    CONSTRAINT material_translations_title_check CHECK (((char_length(title) >= 1) AND (char_length(title) <= 200))),
    CONSTRAINT material_translations_version_check CHECK ((version >= 1))
);


--
-- Name: materials; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.materials (
    id uuid DEFAULT gen_random_uuid() NOT NULL,
    topic_id uuid NOT NULL,
    slug text NOT NULL,
    "position" integer NOT NULL,
    estimated_minutes integer,
    is_active boolean DEFAULT true NOT NULL,
    deleted_at timestamp with time zone,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL,
    CONSTRAINT materials_estimated_minutes_check CHECK ((estimated_minutes > 0)),
    CONSTRAINT materials_position_check CHECK (("position" >= 0)),
    CONSTRAINT materials_slug_check CHECK ((slug ~ '^[a-z0-9]+(-[a-z0-9]+)*$'::text))
);


--
-- Name: misconception_translations; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.misconception_translations (
    id uuid DEFAULT gen_random_uuid() NOT NULL,
    misconception_id uuid NOT NULL,
    locale text NOT NULL,
    title text NOT NULL,
    description text DEFAULT ''::text NOT NULL,
    status text DEFAULT 'draft'::text NOT NULL,
    version integer DEFAULT 1 NOT NULL,
    reviewer_id uuid,
    reviewed_at timestamp with time zone,
    published_at timestamp with time zone,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL,
    CONSTRAINT misconception_translations_pub_chk CHECK (((status <> 'published'::text) OR ((reviewer_id IS NOT NULL) AND (reviewed_at IS NOT NULL) AND (published_at IS NOT NULL)))),
    CONSTRAINT misconception_translations_status_check CHECK ((status = ANY (ARRAY['draft'::text, 'in_review'::text, 'published'::text, 'archived'::text]))),
    CONSTRAINT misconception_translations_title_check CHECK (((char_length(title) >= 1) AND (char_length(title) <= 200))),
    CONSTRAINT misconception_translations_version_check CHECK ((version >= 1))
);


--
-- Name: misconceptions; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.misconceptions (
    id uuid DEFAULT gen_random_uuid() NOT NULL,
    topic_id uuid NOT NULL,
    code text NOT NULL,
    severity text NOT NULL,
    is_active boolean DEFAULT true NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL,
    CONSTRAINT misconceptions_code_check CHECK ((code ~ '^([a-z][a-z0-9_]*|[A-Z][A-Z0-9_]*)$'::text)),
    CONSTRAINT misconceptions_severity_check CHECK ((severity = ANY (ARRAY['minor'::text, 'critical'::text])))
);


--
-- Name: prompt_contracts; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.prompt_contracts (
    id uuid DEFAULT gen_random_uuid() NOT NULL,
    task_type text NOT NULL,
    version integer NOT NULL,
    contract_id text GENERATED ALWAYS AS (((task_type || '@v'::text) || (version)::text)) STORED,
    locale_policy text DEFAULT 'session_locale'::text NOT NULL,
    system_prompt text NOT NULL,
    user_prompt_template text NOT NULL,
    input_schema jsonb NOT NULL,
    output_schema jsonb NOT NULL,
    parameters jsonb NOT NULL,
    status text DEFAULT 'draft'::text NOT NULL,
    content_sha256 text NOT NULL,
    activated_golden_run_id uuid,
    activation_waiver_reason text,
    created_by uuid,
    activated_at timestamp with time zone,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL,
    CONSTRAINT prompt_contracts_active_chk CHECK (((status <> 'active'::text) OR (activated_at IS NOT NULL))),
    CONSTRAINT prompt_contracts_locale_policy_check CHECK ((locale_policy = ANY (ARRAY['session_locale'::text, 'fixed_locale'::text]))),
    CONSTRAINT prompt_contracts_params_chk CHECK ((jsonb_typeof(parameters) = 'object'::text)),
    CONSTRAINT prompt_contracts_status_check CHECK ((status = ANY (ARRAY['draft'::text, 'candidate'::text, 'active'::text, 'retired'::text]))),
    CONSTRAINT prompt_contracts_version_check CHECK ((version >= 1))
);


--
-- Name: question_concepts; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.question_concepts (
    question_id uuid NOT NULL,
    concept_id uuid NOT NULL,
    weight numeric(5,2) DEFAULT 1 NOT NULL,
    is_required boolean DEFAULT false NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL,
    CONSTRAINT question_concepts_weight_check CHECK ((weight > (0)::numeric))
);


--
-- Name: question_translations; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.question_translations (
    id uuid DEFAULT gen_random_uuid() NOT NULL,
    question_id uuid NOT NULL,
    locale text NOT NULL,
    prompt text NOT NULL,
    reference_answer text DEFAULT ''::text NOT NULL,
    evaluation_notes text DEFAULT ''::text NOT NULL,
    status text DEFAULT 'draft'::text NOT NULL,
    version integer DEFAULT 1 NOT NULL,
    reviewer_id uuid,
    reviewed_at timestamp with time zone,
    published_at timestamp with time zone,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL,
    CONSTRAINT question_translations_prompt_check CHECK (((char_length(prompt) >= 1) AND (char_length(prompt) <= 4000))),
    CONSTRAINT question_translations_pub_chk CHECK (((status <> 'published'::text) OR ((reviewer_id IS NOT NULL) AND (reviewed_at IS NOT NULL) AND (published_at IS NOT NULL) AND (char_length(reference_answer) > 0)))),
    CONSTRAINT question_translations_status_check CHECK ((status = ANY (ARRAY['draft'::text, 'in_review'::text, 'published'::text, 'archived'::text]))),
    CONSTRAINT question_translations_version_check CHECK ((version >= 1))
);


--
-- Name: questions; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.questions (
    id uuid DEFAULT gen_random_uuid() NOT NULL,
    topic_id uuid NOT NULL,
    cognitive_level text NOT NULL,
    difficulty smallint DEFAULT 1 NOT NULL,
    purpose text DEFAULT 'both'::text NOT NULL,
    weight numeric(5,2) DEFAULT 1 NOT NULL,
    is_active boolean DEFAULT true NOT NULL,
    deleted_at timestamp with time zone,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL,
    CONSTRAINT questions_cognitive_level_check CHECK ((cognitive_level = ANY (ARRAY['recall'::text, 'conceptual'::text, 'application'::text, 'scenario'::text]))),
    CONSTRAINT questions_difficulty_check CHECK (((difficulty >= 1) AND (difficulty <= 3))),
    CONSTRAINT questions_purpose_check CHECK ((purpose = ANY (ARRAY['assessment'::text, 'interview'::text, 'both'::text]))),
    CONSTRAINT questions_weight_check CHECK ((weight > (0)::numeric))
);


--
-- Name: rate_buckets; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.rate_buckets (
    key text NOT NULL,
    tokens double precision NOT NULL,
    updated_at timestamp with time zone NOT NULL,
    CONSTRAINT rate_buckets_tokens_check CHECK ((tokens >= (0)::double precision))
);


--
-- Name: refresh_tokens; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.refresh_tokens (
    id uuid DEFAULT gen_random_uuid() NOT NULL,
    user_id uuid NOT NULL,
    family_id uuid NOT NULL,
    token_hash bytea NOT NULL,
    expires_at timestamp with time zone NOT NULL,
    revoked_at timestamp with time zone,
    revoked_reason text,
    replaced_by uuid,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL,
    CONSTRAINT refresh_revoked_pair CHECK (((revoked_at IS NULL) = (revoked_reason IS NULL))),
    CONSTRAINT refresh_tokens_revoked_reason_check CHECK ((revoked_reason = ANY (ARRAY['rotated'::text, 'logout'::text, 'reuse_detected'::text, 'user_deactivated'::text, 'password_changed'::text])))
);


--
-- Name: roadmap_topics; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.roadmap_topics (
    roadmap_id uuid NOT NULL,
    topic_id uuid NOT NULL,
    "position" integer NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL,
    CONSTRAINT roadmap_topics_position_check CHECK (("position" >= 0))
);


--
-- Name: roadmap_translations; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.roadmap_translations (
    id uuid DEFAULT gen_random_uuid() NOT NULL,
    roadmap_id uuid NOT NULL,
    locale text NOT NULL,
    title text NOT NULL,
    description text DEFAULT ''::text NOT NULL,
    status text DEFAULT 'draft'::text NOT NULL,
    version integer DEFAULT 1 NOT NULL,
    reviewer_id uuid,
    reviewed_at timestamp with time zone,
    published_at timestamp with time zone,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL,
    CONSTRAINT roadmap_translations_pub_chk CHECK (((status <> 'published'::text) OR ((reviewer_id IS NOT NULL) AND (reviewed_at IS NOT NULL) AND (published_at IS NOT NULL)))),
    CONSTRAINT roadmap_translations_status_check CHECK ((status = ANY (ARRAY['draft'::text, 'in_review'::text, 'published'::text, 'archived'::text]))),
    CONSTRAINT roadmap_translations_title_check CHECK (((char_length(title) >= 1) AND (char_length(title) <= 200))),
    CONSTRAINT roadmap_translations_version_check CHECK ((version >= 1))
);


--
-- Name: roadmaps; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.roadmaps (
    id uuid DEFAULT gen_random_uuid() NOT NULL,
    slug text NOT NULL,
    is_active boolean DEFAULT true NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL,
    CONSTRAINT roadmaps_slug_check CHECK ((slug ~ '^[a-z0-9]+(-[a-z0-9]+)*$'::text))
);


--
-- Name: schema_migrations; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.schema_migrations (
    version bigint NOT NULL,
    dirty boolean NOT NULL
);


--
-- Name: supported_locales; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.supported_locales (
    code text NOT NULL,
    display_name text NOT NULL,
    is_default boolean DEFAULT false NOT NULL,
    is_enabled boolean DEFAULT true NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL,
    CONSTRAINT supported_locales_code_check CHECK ((code ~ '^[a-z]{2,3}$'::text))
);


--
-- Name: topic_attempts; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.topic_attempts (
    id uuid DEFAULT gen_random_uuid() NOT NULL,
    user_id uuid NOT NULL,
    topic_id uuid NOT NULL,
    source_type text NOT NULL,
    assessment_attempt_id uuid,
    interview_session_id uuid,
    score_pct numeric(5,2) NOT NULL,
    has_application_question boolean NOT NULL,
    scored_at timestamp with time zone NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    CONSTRAINT topic_attempts_score_pct_check CHECK (((score_pct >= (0)::numeric) AND (score_pct <= (100)::numeric))),
    CONSTRAINT topic_attempts_source_chk CHECK ((((source_type = 'assessment'::text) AND (assessment_attempt_id IS NOT NULL) AND (interview_session_id IS NULL)) OR ((source_type = 'interview'::text) AND (interview_session_id IS NOT NULL) AND (assessment_attempt_id IS NULL)))),
    CONSTRAINT topic_attempts_source_type_check CHECK ((source_type = ANY (ARRAY['assessment'::text, 'interview'::text])))
);


--
-- Name: topic_prerequisites; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.topic_prerequisites (
    topic_id uuid NOT NULL,
    prerequisite_topic_id uuid NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    CONSTRAINT topic_prerequisites_check CHECK ((topic_id <> prerequisite_topic_id))
);


--
-- Name: topic_translations; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.topic_translations (
    id uuid DEFAULT gen_random_uuid() NOT NULL,
    topic_id uuid NOT NULL,
    locale text NOT NULL,
    title text NOT NULL,
    summary text DEFAULT ''::text NOT NULL,
    status text DEFAULT 'draft'::text NOT NULL,
    version integer DEFAULT 1 NOT NULL,
    reviewer_id uuid,
    reviewed_at timestamp with time zone,
    published_at timestamp with time zone,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL,
    CONSTRAINT topic_translations_pub_chk CHECK (((status <> 'published'::text) OR ((reviewer_id IS NOT NULL) AND (reviewed_at IS NOT NULL) AND (published_at IS NOT NULL)))),
    CONSTRAINT topic_translations_status_check CHECK ((status = ANY (ARRAY['draft'::text, 'in_review'::text, 'published'::text, 'archived'::text]))),
    CONSTRAINT topic_translations_title_check CHECK (((char_length(title) >= 1) AND (char_length(title) <= 200))),
    CONSTRAINT topic_translations_version_check CHECK ((version >= 1))
);


--
-- Name: topics; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.topics (
    id uuid DEFAULT gen_random_uuid() NOT NULL,
    slug text NOT NULL,
    "position" integer NOT NULL,
    difficulty text DEFAULT 'beginner'::text NOT NULL,
    is_active boolean DEFAULT true NOT NULL,
    deleted_at timestamp with time zone,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL,
    CONSTRAINT topics_difficulty_check CHECK ((difficulty = ANY (ARRAY['beginner'::text, 'intermediate'::text, 'advanced'::text]))),
    CONSTRAINT topics_position_check CHECK (("position" >= 0)),
    CONSTRAINT topics_slug_check CHECK ((slug ~ '^[a-z0-9]+(-[a-z0-9]+)*$'::text))
);


--
-- Name: user_misconceptions; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.user_misconceptions (
    id uuid DEFAULT gen_random_uuid() NOT NULL,
    user_id uuid NOT NULL,
    misconception_id uuid NOT NULL,
    source_evaluation_id uuid,
    status text DEFAULT 'detected'::text NOT NULL,
    detected_at timestamp with time zone DEFAULT now() NOT NULL,
    resolved_at timestamp with time zone,
    resolved_by_evaluation_id uuid,
    resolved_by uuid,
    resolution_reason text,
    consecutive_resolve_count smallint DEFAULT 0 NOT NULL,
    occurrences integer DEFAULT 1 NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL,
    CONSTRAINT user_misconceptions_consecutive_resolve_count_check CHECK ((consecutive_resolve_count >= 0)),
    CONSTRAINT user_misconceptions_manual_chk CHECK (((resolved_by IS NULL) OR (resolution_reason IS NOT NULL))),
    CONSTRAINT user_misconceptions_occurrences_check CHECK ((occurrences >= 1)),
    CONSTRAINT user_misconceptions_resolved_chk CHECK (((status = 'resolved'::text) = (resolved_at IS NOT NULL))),
    CONSTRAINT user_misconceptions_status_check CHECK ((status = ANY (ARRAY['detected'::text, 'resolved'::text])))
);


--
-- Name: user_roadmaps; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.user_roadmaps (
    user_id uuid NOT NULL,
    roadmap_id uuid NOT NULL,
    selected_at timestamp with time zone DEFAULT now() NOT NULL
);


--
-- Name: users; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.users (
    id uuid DEFAULT gen_random_uuid() NOT NULL,
    email text NOT NULL,
    password_hash text NOT NULL,
    display_name text,
    role text DEFAULT 'user'::text NOT NULL,
    preferred_locale text,
    is_active boolean DEFAULT true NOT NULL,
    last_login_at timestamp with time zone,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL,
    CONSTRAINT users_email_check CHECK (((char_length(email) >= 3) AND (char_length(email) <= 254))),
    CONSTRAINT users_role_check CHECK ((role = ANY (ARRAY['user'::text, 'admin'::text])))
);


--
-- Name: ai_injection_events ai_injection_events_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.ai_injection_events
    ADD CONSTRAINT ai_injection_events_pkey PRIMARY KEY (id);


--
-- Name: ai_provider_settings ai_provider_settings_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.ai_provider_settings
    ADD CONSTRAINT ai_provider_settings_pkey PRIMARY KEY (id);


--
-- Name: ai_provider_settings ai_provider_settings_uk; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.ai_provider_settings
    ADD CONSTRAINT ai_provider_settings_uk UNIQUE (provider_name, model_name);


--
-- Name: ai_task_types ai_task_types_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.ai_task_types
    ADD CONSTRAINT ai_task_types_pkey PRIMARY KEY (task_type);


--
-- Name: ai_tasks ai_tasks_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.ai_tasks
    ADD CONSTRAINT ai_tasks_pkey PRIMARY KEY (id);


--
-- Name: ai_usage_logs ai_usage_logs_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.ai_usage_logs
    ADD CONSTRAINT ai_usage_logs_pkey PRIMARY KEY (id);


--
-- Name: answer_evaluation_misconceptions answer_evaluation_misconceptions_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.answer_evaluation_misconceptions
    ADD CONSTRAINT answer_evaluation_misconceptions_pkey PRIMARY KEY (answer_evaluation_id, misconception_id);


--
-- Name: answer_evaluations answer_evaluations_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.answer_evaluations
    ADD CONSTRAINT answer_evaluations_pkey PRIMARY KEY (id);


--
-- Name: app_settings app_settings_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.app_settings
    ADD CONSTRAINT app_settings_pkey PRIMARY KEY (key);


--
-- Name: assessment_answers assessment_answers_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.assessment_answers
    ADD CONSTRAINT assessment_answers_pkey PRIMARY KEY (id);


--
-- Name: assessment_answers assessment_answers_pos_uk; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.assessment_answers
    ADD CONSTRAINT assessment_answers_pos_uk UNIQUE (attempt_id, "position");


--
-- Name: assessment_answers assessment_answers_q_uk; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.assessment_answers
    ADD CONSTRAINT assessment_answers_q_uk UNIQUE (attempt_id, question_id);


--
-- Name: assessment_attempts assessment_attempts_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.assessment_attempts
    ADD CONSTRAINT assessment_attempts_pkey PRIMARY KEY (id);


--
-- Name: concept_translations concept_translations_locale_uk; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.concept_translations
    ADD CONSTRAINT concept_translations_locale_uk UNIQUE (concept_id, locale);


--
-- Name: concept_translations concept_translations_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.concept_translations
    ADD CONSTRAINT concept_translations_pkey PRIMARY KEY (id);


--
-- Name: concepts concepts_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.concepts
    ADD CONSTRAINT concepts_pkey PRIMARY KEY (id);


--
-- Name: concepts concepts_topic_code_uk; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.concepts
    ADD CONSTRAINT concepts_topic_code_uk UNIQUE (topic_id, code);


--
-- Name: golden_cases golden_cases_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.golden_cases
    ADD CONSTRAINT golden_cases_pkey PRIMARY KEY (id);


--
-- Name: golden_recordings golden_recordings_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.golden_recordings
    ADD CONSTRAINT golden_recordings_pkey PRIMARY KEY (id);


--
-- Name: golden_recordings golden_recordings_uk; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.golden_recordings
    ADD CONSTRAINT golden_recordings_uk UNIQUE (prompt_contract_id, model_name, golden_case_id);


--
-- Name: golden_run_results golden_run_results_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.golden_run_results
    ADD CONSTRAINT golden_run_results_pkey PRIMARY KEY (id);


--
-- Name: golden_run_results golden_run_results_uk; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.golden_run_results
    ADD CONSTRAINT golden_run_results_uk UNIQUE (golden_run_id, golden_case_id);


--
-- Name: golden_runs golden_runs_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.golden_runs
    ADD CONSTRAINT golden_runs_pkey PRIMARY KEY (id);


--
-- Name: interview_evaluations interview_evaluations_interview_session_id_key; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.interview_evaluations
    ADD CONSTRAINT interview_evaluations_interview_session_id_key UNIQUE (interview_session_id);


--
-- Name: interview_evaluations interview_evaluations_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.interview_evaluations
    ADD CONSTRAINT interview_evaluations_pkey PRIMARY KEY (id);


--
-- Name: interview_messages interview_messages_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.interview_messages
    ADD CONSTRAINT interview_messages_pkey PRIMARY KEY (id);


--
-- Name: interview_messages interview_messages_seq_uk; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.interview_messages
    ADD CONSTRAINT interview_messages_seq_uk UNIQUE (interview_session_id, sequence);


--
-- Name: interview_profiles interview_profiles_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.interview_profiles
    ADD CONSTRAINT interview_profiles_pkey PRIMARY KEY (interview_type);


--
-- Name: interview_sessions interview_sessions_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.interview_sessions
    ADD CONSTRAINT interview_sessions_pkey PRIMARY KEY (id);


--
-- Name: invitations invitations_code_hash_key; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.invitations
    ADD CONSTRAINT invitations_code_hash_key UNIQUE (code_hash);


--
-- Name: invitations invitations_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.invitations
    ADD CONSTRAINT invitations_pkey PRIMARY KEY (id);


--
-- Name: mastery_records mastery_records_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.mastery_records
    ADD CONSTRAINT mastery_records_pkey PRIMARY KEY (id);


--
-- Name: mastery_records mastery_records_user_topic_uk; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.mastery_records
    ADD CONSTRAINT mastery_records_user_topic_uk UNIQUE (user_id, topic_id);


--
-- Name: material_translations material_translations_locale_uk; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.material_translations
    ADD CONSTRAINT material_translations_locale_uk UNIQUE (material_id, locale);


--
-- Name: material_translations material_translations_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.material_translations
    ADD CONSTRAINT material_translations_pkey PRIMARY KEY (id);


--
-- Name: materials materials_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.materials
    ADD CONSTRAINT materials_pkey PRIMARY KEY (id);


--
-- Name: misconception_translations misconception_translations_locale_uk; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.misconception_translations
    ADD CONSTRAINT misconception_translations_locale_uk UNIQUE (misconception_id, locale);


--
-- Name: misconception_translations misconception_translations_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.misconception_translations
    ADD CONSTRAINT misconception_translations_pkey PRIMARY KEY (id);


--
-- Name: misconceptions misconceptions_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.misconceptions
    ADD CONSTRAINT misconceptions_pkey PRIMARY KEY (id);


--
-- Name: misconceptions misconceptions_topic_code_uk; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.misconceptions
    ADD CONSTRAINT misconceptions_topic_code_uk UNIQUE (topic_id, code);


--
-- Name: prompt_contracts prompt_contracts_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.prompt_contracts
    ADD CONSTRAINT prompt_contracts_pkey PRIMARY KEY (id);


--
-- Name: prompt_contracts prompt_contracts_ver_uk; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.prompt_contracts
    ADD CONSTRAINT prompt_contracts_ver_uk UNIQUE (task_type, version);


--
-- Name: question_concepts question_concepts_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.question_concepts
    ADD CONSTRAINT question_concepts_pkey PRIMARY KEY (question_id, concept_id);


--
-- Name: question_translations question_translations_locale_uk; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.question_translations
    ADD CONSTRAINT question_translations_locale_uk UNIQUE (question_id, locale);


--
-- Name: question_translations question_translations_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.question_translations
    ADD CONSTRAINT question_translations_pkey PRIMARY KEY (id);


--
-- Name: questions questions_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.questions
    ADD CONSTRAINT questions_pkey PRIMARY KEY (id);


--
-- Name: rate_buckets rate_buckets_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.rate_buckets
    ADD CONSTRAINT rate_buckets_pkey PRIMARY KEY (key);


--
-- Name: refresh_tokens refresh_tokens_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.refresh_tokens
    ADD CONSTRAINT refresh_tokens_pkey PRIMARY KEY (id);


--
-- Name: refresh_tokens refresh_tokens_token_hash_key; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.refresh_tokens
    ADD CONSTRAINT refresh_tokens_token_hash_key UNIQUE (token_hash);


--
-- Name: roadmap_topics roadmap_topics_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.roadmap_topics
    ADD CONSTRAINT roadmap_topics_pkey PRIMARY KEY (roadmap_id, topic_id);


--
-- Name: roadmap_topics roadmap_topics_pos_uk; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.roadmap_topics
    ADD CONSTRAINT roadmap_topics_pos_uk UNIQUE (roadmap_id, "position") DEFERRABLE INITIALLY DEFERRED;


--
-- Name: roadmap_translations roadmap_translations_locale_uk; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.roadmap_translations
    ADD CONSTRAINT roadmap_translations_locale_uk UNIQUE (roadmap_id, locale);


--
-- Name: roadmap_translations roadmap_translations_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.roadmap_translations
    ADD CONSTRAINT roadmap_translations_pkey PRIMARY KEY (id);


--
-- Name: roadmaps roadmaps_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.roadmaps
    ADD CONSTRAINT roadmaps_pkey PRIMARY KEY (id);


--
-- Name: roadmaps roadmaps_slug_uk; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.roadmaps
    ADD CONSTRAINT roadmaps_slug_uk UNIQUE (slug);


--
-- Name: schema_migrations schema_migrations_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.schema_migrations
    ADD CONSTRAINT schema_migrations_pkey PRIMARY KEY (version);


--
-- Name: supported_locales supported_locales_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.supported_locales
    ADD CONSTRAINT supported_locales_pkey PRIMARY KEY (code);


--
-- Name: topic_attempts topic_attempts_aa_uk; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.topic_attempts
    ADD CONSTRAINT topic_attempts_aa_uk UNIQUE (assessment_attempt_id);


--
-- Name: topic_attempts topic_attempts_is_uk; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.topic_attempts
    ADD CONSTRAINT topic_attempts_is_uk UNIQUE (interview_session_id, topic_id);


--
-- Name: topic_attempts topic_attempts_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.topic_attempts
    ADD CONSTRAINT topic_attempts_pkey PRIMARY KEY (id);


--
-- Name: topic_prerequisites topic_prerequisites_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.topic_prerequisites
    ADD CONSTRAINT topic_prerequisites_pkey PRIMARY KEY (topic_id, prerequisite_topic_id);


--
-- Name: topic_translations topic_translations_locale_uk; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.topic_translations
    ADD CONSTRAINT topic_translations_locale_uk UNIQUE (topic_id, locale);


--
-- Name: topic_translations topic_translations_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.topic_translations
    ADD CONSTRAINT topic_translations_pkey PRIMARY KEY (id);


--
-- Name: topics topics_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.topics
    ADD CONSTRAINT topics_pkey PRIMARY KEY (id);


--
-- Name: user_misconceptions user_misconceptions_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.user_misconceptions
    ADD CONSTRAINT user_misconceptions_pkey PRIMARY KEY (id);


--
-- Name: user_misconceptions user_misconceptions_uk; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.user_misconceptions
    ADD CONSTRAINT user_misconceptions_uk UNIQUE (user_id, misconception_id);


--
-- Name: user_roadmaps user_roadmaps_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.user_roadmaps
    ADD CONSTRAINT user_roadmaps_pkey PRIMARY KEY (user_id, roadmap_id);


--
-- Name: users users_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.users
    ADD CONSTRAINT users_pkey PRIMARY KEY (id);


--
-- Name: aem_misconception_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX aem_misconception_idx ON public.answer_evaluation_misconceptions USING btree (misconception_id);


--
-- Name: ai_injection_events_user_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX ai_injection_events_user_idx ON public.ai_injection_events USING btree (user_id, created_at DESC);


--
-- Name: ai_provider_settings_order_uk; Type: INDEX; Schema: public; Owner: -
--

CREATE UNIQUE INDEX ai_provider_settings_order_uk ON public.ai_provider_settings USING btree (fallback_order) WHERE is_enabled;


--
-- Name: ai_tasks_dequeue_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX ai_tasks_dequeue_idx ON public.ai_tasks USING btree (priority, run_after, created_at) WHERE (status = 'queued'::text);


--
-- Name: ai_tasks_idem_uk; Type: INDEX; Schema: public; Owner: -
--

CREATE UNIQUE INDEX ai_tasks_idem_uk ON public.ai_tasks USING btree (user_id, idempotency_key) WHERE (idempotency_key IS NOT NULL);


--
-- Name: ai_tasks_reaper_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX ai_tasks_reaper_idx ON public.ai_tasks USING btree (locked_at) WHERE (status = 'processing'::text);


--
-- Name: ai_tasks_resource_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX ai_tasks_resource_idx ON public.ai_tasks USING btree (resource_type, resource_id);


--
-- Name: ai_tasks_user_active_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX ai_tasks_user_active_idx ON public.ai_tasks USING btree (user_id) WHERE (status = ANY (ARRAY['queued'::text, 'processing'::text]));


--
-- Name: ai_usage_task_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX ai_usage_task_idx ON public.ai_usage_logs USING btree (ai_task_id);


--
-- Name: ai_usage_time_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX ai_usage_time_idx ON public.ai_usage_logs USING btree (created_at);


--
-- Name: ai_usage_user_time_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX ai_usage_user_time_idx ON public.ai_usage_logs USING btree (user_id, created_at);


--
-- Name: answer_evaluations_aa_uk; Type: INDEX; Schema: public; Owner: -
--

CREATE UNIQUE INDEX answer_evaluations_aa_uk ON public.answer_evaluations USING btree (assessment_answer_id) WHERE (assessment_answer_id IS NOT NULL);


--
-- Name: answer_evaluations_im_uk; Type: INDEX; Schema: public; Owner: -
--

CREATE UNIQUE INDEX answer_evaluations_im_uk ON public.answer_evaluations USING btree (interview_message_id) WHERE (interview_message_id IS NOT NULL);


--
-- Name: answer_evaluations_user_topic_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX answer_evaluations_user_topic_idx ON public.answer_evaluations USING btree (user_id, topic_id, created_at DESC);


--
-- Name: assessment_answers_failed_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX assessment_answers_failed_idx ON public.assessment_answers USING btree (evaluation_failed_at) WHERE (evaluation_status = 'failed'::text);


--
-- Name: assessment_answers_open_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX assessment_answers_open_idx ON public.assessment_answers USING btree (attempt_id) WHERE (evaluation_status = ANY (ARRAY['pending'::text, 'failed'::text]));


--
-- Name: assessment_attempts_topic_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX assessment_attempts_topic_idx ON public.assessment_attempts USING btree (user_id, topic_id, status);


--
-- Name: assessment_attempts_user_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX assessment_attempts_user_idx ON public.assessment_attempts USING btree (user_id, created_at DESC, id DESC);


--
-- Name: concept_translations_published_uk; Type: INDEX; Schema: public; Owner: -
--

CREATE UNIQUE INDEX concept_translations_published_uk ON public.concept_translations USING btree (concept_id, locale) WHERE (status = 'published'::text);


--
-- Name: golden_cases_active_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX golden_cases_active_idx ON public.golden_cases USING btree (category) WHERE is_active;


--
-- Name: golden_cases_question_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX golden_cases_question_idx ON public.golden_cases USING btree (question_id);


--
-- Name: golden_recordings_source_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX golden_recordings_source_idx ON public.golden_recordings USING btree (source_run_id);


--
-- Name: golden_runs_contract_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX golden_runs_contract_idx ON public.golden_runs USING btree (prompt_contract_id, started_at DESC);


--
-- Name: golden_runs_live_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX golden_runs_live_idx ON public.golden_runs USING btree (prompt_contract_id, started_at DESC) WHERE (run_mode = 'live'::text);


--
-- Name: interview_messages_failed_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX interview_messages_failed_idx ON public.interview_messages USING btree (evaluation_failed_at) WHERE (evaluation_status = 'failed'::text);


--
-- Name: interview_messages_one_child_uk; Type: INDEX; Schema: public; Owner: -
--

CREATE UNIQUE INDEX interview_messages_one_child_uk ON public.interview_messages USING btree (parent_message_id) WHERE (parent_message_id IS NOT NULL);


--
-- Name: interview_messages_open_eval_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX interview_messages_open_eval_idx ON public.interview_messages USING btree (interview_session_id) WHERE (evaluation_status = ANY (ARRAY['pending'::text, 'failed'::text]));


--
-- Name: interview_messages_question_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX interview_messages_question_idx ON public.interview_messages USING btree (interview_session_id, question_id);


--
-- Name: interview_sessions_expiry_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX interview_sessions_expiry_idx ON public.interview_sessions USING btree (expires_at) WHERE (status = 'PAUSED'::text);


--
-- Name: interview_sessions_retention_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX interview_sessions_retention_idx ON public.interview_sessions USING btree (retention_until) WHERE ((retention_until IS NOT NULL) AND (deleted_at IS NULL));


--
-- Name: interview_sessions_retry_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX interview_sessions_retry_idx ON public.interview_sessions USING btree (retry_of_session_id) WHERE (retry_of_session_id IS NOT NULL);


--
-- Name: interview_sessions_user_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX interview_sessions_user_idx ON public.interview_sessions USING btree (user_id, created_at DESC, id DESC) WHERE (deleted_at IS NULL);


--
-- Name: invitations_created_by_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX invitations_created_by_idx ON public.invitations USING btree (created_by, created_at DESC);


--
-- Name: invitations_open_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX invitations_open_idx ON public.invitations USING btree (expires_at) WHERE ((used_at IS NULL) AND (revoked_at IS NULL));


--
-- Name: mastery_records_user_status_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX mastery_records_user_status_idx ON public.mastery_records USING btree (user_id, status);


--
-- Name: material_translations_published_uk; Type: INDEX; Schema: public; Owner: -
--

CREATE UNIQUE INDEX material_translations_published_uk ON public.material_translations USING btree (material_id, locale) WHERE (status = 'published'::text);


--
-- Name: materials_topic_id_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX materials_topic_id_idx ON public.materials USING btree (topic_id);


--
-- Name: materials_topic_pos_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX materials_topic_pos_idx ON public.materials USING btree (topic_id, "position", id) WHERE ((deleted_at IS NULL) AND is_active);


--
-- Name: materials_topic_slug_uk; Type: INDEX; Schema: public; Owner: -
--

CREATE UNIQUE INDEX materials_topic_slug_uk ON public.materials USING btree (topic_id, slug) WHERE (deleted_at IS NULL);


--
-- Name: misconception_translations_published_uk; Type: INDEX; Schema: public; Owner: -
--

CREATE UNIQUE INDEX misconception_translations_published_uk ON public.misconception_translations USING btree (misconception_id, locale) WHERE (status = 'published'::text);


--
-- Name: misconceptions_topic_active_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX misconceptions_topic_active_idx ON public.misconceptions USING btree (topic_id) WHERE is_active;


--
-- Name: prompt_contracts_one_active_uk; Type: INDEX; Schema: public; Owner: -
--

CREATE UNIQUE INDEX prompt_contracts_one_active_uk ON public.prompt_contracts USING btree (task_type) WHERE (status = 'active'::text);


--
-- Name: question_concepts_concept_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX question_concepts_concept_idx ON public.question_concepts USING btree (concept_id);


--
-- Name: question_translations_published_uk; Type: INDEX; Schema: public; Owner: -
--

CREATE UNIQUE INDEX question_translations_published_uk ON public.question_translations USING btree (question_id, locale) WHERE (status = 'published'::text);


--
-- Name: questions_topic_id_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX questions_topic_id_idx ON public.questions USING btree (topic_id);


--
-- Name: questions_topic_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX questions_topic_idx ON public.questions USING btree (topic_id, cognitive_level) WHERE ((deleted_at IS NULL) AND is_active);


--
-- Name: rate_buckets_updated_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX rate_buckets_updated_idx ON public.rate_buckets USING btree (updated_at);


--
-- Name: refresh_tokens_expiry_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX refresh_tokens_expiry_idx ON public.refresh_tokens USING btree (expires_at);


--
-- Name: refresh_tokens_family_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX refresh_tokens_family_idx ON public.refresh_tokens USING btree (family_id);


--
-- Name: refresh_tokens_user_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX refresh_tokens_user_idx ON public.refresh_tokens USING btree (user_id) WHERE (revoked_at IS NULL);


--
-- Name: roadmap_topics_topic_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX roadmap_topics_topic_idx ON public.roadmap_topics USING btree (topic_id);


--
-- Name: roadmap_translations_published_uk; Type: INDEX; Schema: public; Owner: -
--

CREATE UNIQUE INDEX roadmap_translations_published_uk ON public.roadmap_translations USING btree (roadmap_id, locale) WHERE (status = 'published'::text);


--
-- Name: supported_locales_one_default_uk; Type: INDEX; Schema: public; Owner: -
--

CREATE UNIQUE INDEX supported_locales_one_default_uk ON public.supported_locales USING btree (is_default) WHERE is_default;


--
-- Name: topic_attempts_window_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX topic_attempts_window_idx ON public.topic_attempts USING btree (user_id, topic_id, scored_at DESC, id DESC);


--
-- Name: topic_prerequisites_prereq_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX topic_prerequisites_prereq_idx ON public.topic_prerequisites USING btree (prerequisite_topic_id);


--
-- Name: topic_translations_published_uk; Type: INDEX; Schema: public; Owner: -
--

CREATE UNIQUE INDEX topic_translations_published_uk ON public.topic_translations USING btree (topic_id, locale) WHERE (status = 'published'::text);


--
-- Name: topics_position_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX topics_position_idx ON public.topics USING btree ("position", id) WHERE ((deleted_at IS NULL) AND is_active);


--
-- Name: topics_slug_uk; Type: INDEX; Schema: public; Owner: -
--

CREATE UNIQUE INDEX topics_slug_uk ON public.topics USING btree (slug) WHERE (deleted_at IS NULL);


--
-- Name: user_misconceptions_open_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX user_misconceptions_open_idx ON public.user_misconceptions USING btree (user_id) WHERE (status = 'detected'::text);


--
-- Name: user_roadmaps_roadmap_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX user_roadmaps_roadmap_idx ON public.user_roadmaps USING btree (roadmap_id);


--
-- Name: users_email_lower_uk; Type: INDEX; Schema: public; Owner: -
--

CREATE UNIQUE INDEX users_email_lower_uk ON public.users USING btree (lower(email));


--
-- Name: app_settings trg_app_settings_mark_changed; Type: TRIGGER; Schema: public; Owner: -
--

CREATE TRIGGER trg_app_settings_mark_changed BEFORE UPDATE OF value ON public.app_settings FOR EACH ROW EXECUTE FUNCTION public.app_settings_mark_changed();


--
-- Name: assessment_answers trg_assessment_answers_frozen; Type: TRIGGER; Schema: public; Owner: -
--

CREATE TRIGGER trg_assessment_answers_frozen BEFORE UPDATE OF answer_text ON public.assessment_answers FOR EACH ROW EXECUTE FUNCTION public.answer_text_frozen_after_submit();


--
-- Name: misconceptions trg_misconceptions_immutable; Type: TRIGGER; Schema: public; Owner: -
--

CREATE TRIGGER trg_misconceptions_immutable BEFORE UPDATE ON public.misconceptions FOR EACH ROW EXECUTE FUNCTION public.misconceptions_immutable();


--
-- Name: prompt_contracts trg_prompt_contracts_immutable; Type: TRIGGER; Schema: public; Owner: -
--

CREATE TRIGGER trg_prompt_contracts_immutable BEFORE UPDATE ON public.prompt_contracts FOR EACH ROW EXECUTE FUNCTION public.prompt_contracts_immutable();


--
-- Name: concept_translations trg_reviewer_admin; Type: TRIGGER; Schema: public; Owner: -
--

CREATE TRIGGER trg_reviewer_admin BEFORE INSERT OR UPDATE OF reviewer_id ON public.concept_translations FOR EACH ROW EXECUTE FUNCTION public.enforce_reviewer_admin();


--
-- Name: material_translations trg_reviewer_admin; Type: TRIGGER; Schema: public; Owner: -
--

CREATE TRIGGER trg_reviewer_admin BEFORE INSERT OR UPDATE OF reviewer_id ON public.material_translations FOR EACH ROW EXECUTE FUNCTION public.enforce_reviewer_admin();


--
-- Name: misconception_translations trg_reviewer_admin; Type: TRIGGER; Schema: public; Owner: -
--

CREATE TRIGGER trg_reviewer_admin BEFORE INSERT OR UPDATE OF reviewer_id ON public.misconception_translations FOR EACH ROW EXECUTE FUNCTION public.enforce_reviewer_admin();


--
-- Name: question_translations trg_reviewer_admin; Type: TRIGGER; Schema: public; Owner: -
--

CREATE TRIGGER trg_reviewer_admin BEFORE INSERT OR UPDATE OF reviewer_id ON public.question_translations FOR EACH ROW EXECUTE FUNCTION public.enforce_reviewer_admin();


--
-- Name: roadmap_translations trg_reviewer_admin; Type: TRIGGER; Schema: public; Owner: -
--

CREATE TRIGGER trg_reviewer_admin BEFORE INSERT OR UPDATE OF reviewer_id ON public.roadmap_translations FOR EACH ROW EXECUTE FUNCTION public.enforce_reviewer_admin();


--
-- Name: topic_translations trg_reviewer_admin; Type: TRIGGER; Schema: public; Owner: -
--

CREATE TRIGGER trg_reviewer_admin BEFORE INSERT OR UPDATE OF reviewer_id ON public.topic_translations FOR EACH ROW EXECUTE FUNCTION public.enforce_reviewer_admin();


--
-- Name: ai_provider_settings trg_set_updated_at; Type: TRIGGER; Schema: public; Owner: -
--

CREATE TRIGGER trg_set_updated_at BEFORE UPDATE ON public.ai_provider_settings FOR EACH ROW EXECUTE FUNCTION public.set_updated_at();


--
-- Name: ai_tasks trg_set_updated_at; Type: TRIGGER; Schema: public; Owner: -
--

CREATE TRIGGER trg_set_updated_at BEFORE UPDATE ON public.ai_tasks FOR EACH ROW EXECUTE FUNCTION public.set_updated_at();


--
-- Name: app_settings trg_set_updated_at; Type: TRIGGER; Schema: public; Owner: -
--

CREATE TRIGGER trg_set_updated_at BEFORE UPDATE ON public.app_settings FOR EACH ROW EXECUTE FUNCTION public.set_updated_at();


--
-- Name: assessment_answers trg_set_updated_at; Type: TRIGGER; Schema: public; Owner: -
--

CREATE TRIGGER trg_set_updated_at BEFORE UPDATE ON public.assessment_answers FOR EACH ROW EXECUTE FUNCTION public.set_updated_at();


--
-- Name: assessment_attempts trg_set_updated_at; Type: TRIGGER; Schema: public; Owner: -
--

CREATE TRIGGER trg_set_updated_at BEFORE UPDATE ON public.assessment_attempts FOR EACH ROW EXECUTE FUNCTION public.set_updated_at();


--
-- Name: concept_translations trg_set_updated_at; Type: TRIGGER; Schema: public; Owner: -
--

CREATE TRIGGER trg_set_updated_at BEFORE UPDATE ON public.concept_translations FOR EACH ROW EXECUTE FUNCTION public.set_updated_at();


--
-- Name: concepts trg_set_updated_at; Type: TRIGGER; Schema: public; Owner: -
--

CREATE TRIGGER trg_set_updated_at BEFORE UPDATE ON public.concepts FOR EACH ROW EXECUTE FUNCTION public.set_updated_at();


--
-- Name: golden_cases trg_set_updated_at; Type: TRIGGER; Schema: public; Owner: -
--

CREATE TRIGGER trg_set_updated_at BEFORE UPDATE ON public.golden_cases FOR EACH ROW EXECUTE FUNCTION public.set_updated_at();


--
-- Name: golden_runs trg_set_updated_at; Type: TRIGGER; Schema: public; Owner: -
--

CREATE TRIGGER trg_set_updated_at BEFORE UPDATE ON public.golden_runs FOR EACH ROW EXECUTE FUNCTION public.set_updated_at();


--
-- Name: interview_profiles trg_set_updated_at; Type: TRIGGER; Schema: public; Owner: -
--

CREATE TRIGGER trg_set_updated_at BEFORE UPDATE ON public.interview_profiles FOR EACH ROW EXECUTE FUNCTION public.set_updated_at();


--
-- Name: interview_sessions trg_set_updated_at; Type: TRIGGER; Schema: public; Owner: -
--

CREATE TRIGGER trg_set_updated_at BEFORE UPDATE ON public.interview_sessions FOR EACH ROW EXECUTE FUNCTION public.set_updated_at();


--
-- Name: invitations trg_set_updated_at; Type: TRIGGER; Schema: public; Owner: -
--

CREATE TRIGGER trg_set_updated_at BEFORE UPDATE ON public.invitations FOR EACH ROW EXECUTE FUNCTION public.set_updated_at();


--
-- Name: mastery_records trg_set_updated_at; Type: TRIGGER; Schema: public; Owner: -
--

CREATE TRIGGER trg_set_updated_at BEFORE UPDATE ON public.mastery_records FOR EACH ROW EXECUTE FUNCTION public.set_updated_at();


--
-- Name: material_translations trg_set_updated_at; Type: TRIGGER; Schema: public; Owner: -
--

CREATE TRIGGER trg_set_updated_at BEFORE UPDATE ON public.material_translations FOR EACH ROW EXECUTE FUNCTION public.set_updated_at();


--
-- Name: materials trg_set_updated_at; Type: TRIGGER; Schema: public; Owner: -
--

CREATE TRIGGER trg_set_updated_at BEFORE UPDATE ON public.materials FOR EACH ROW EXECUTE FUNCTION public.set_updated_at();


--
-- Name: misconception_translations trg_set_updated_at; Type: TRIGGER; Schema: public; Owner: -
--

CREATE TRIGGER trg_set_updated_at BEFORE UPDATE ON public.misconception_translations FOR EACH ROW EXECUTE FUNCTION public.set_updated_at();


--
-- Name: misconceptions trg_set_updated_at; Type: TRIGGER; Schema: public; Owner: -
--

CREATE TRIGGER trg_set_updated_at BEFORE UPDATE ON public.misconceptions FOR EACH ROW EXECUTE FUNCTION public.set_updated_at();


--
-- Name: prompt_contracts trg_set_updated_at; Type: TRIGGER; Schema: public; Owner: -
--

CREATE TRIGGER trg_set_updated_at BEFORE UPDATE ON public.prompt_contracts FOR EACH ROW EXECUTE FUNCTION public.set_updated_at();


--
-- Name: question_concepts trg_set_updated_at; Type: TRIGGER; Schema: public; Owner: -
--

CREATE TRIGGER trg_set_updated_at BEFORE UPDATE ON public.question_concepts FOR EACH ROW EXECUTE FUNCTION public.set_updated_at();


--
-- Name: question_translations trg_set_updated_at; Type: TRIGGER; Schema: public; Owner: -
--

CREATE TRIGGER trg_set_updated_at BEFORE UPDATE ON public.question_translations FOR EACH ROW EXECUTE FUNCTION public.set_updated_at();


--
-- Name: questions trg_set_updated_at; Type: TRIGGER; Schema: public; Owner: -
--

CREATE TRIGGER trg_set_updated_at BEFORE UPDATE ON public.questions FOR EACH ROW EXECUTE FUNCTION public.set_updated_at();


--
-- Name: refresh_tokens trg_set_updated_at; Type: TRIGGER; Schema: public; Owner: -
--

CREATE TRIGGER trg_set_updated_at BEFORE UPDATE ON public.refresh_tokens FOR EACH ROW EXECUTE FUNCTION public.set_updated_at();


--
-- Name: roadmap_topics trg_set_updated_at; Type: TRIGGER; Schema: public; Owner: -
--

CREATE TRIGGER trg_set_updated_at BEFORE UPDATE ON public.roadmap_topics FOR EACH ROW EXECUTE FUNCTION public.set_updated_at();


--
-- Name: roadmap_translations trg_set_updated_at; Type: TRIGGER; Schema: public; Owner: -
--

CREATE TRIGGER trg_set_updated_at BEFORE UPDATE ON public.roadmap_translations FOR EACH ROW EXECUTE FUNCTION public.set_updated_at();


--
-- Name: roadmaps trg_set_updated_at; Type: TRIGGER; Schema: public; Owner: -
--

CREATE TRIGGER trg_set_updated_at BEFORE UPDATE ON public.roadmaps FOR EACH ROW EXECUTE FUNCTION public.set_updated_at();


--
-- Name: supported_locales trg_set_updated_at; Type: TRIGGER; Schema: public; Owner: -
--

CREATE TRIGGER trg_set_updated_at BEFORE UPDATE ON public.supported_locales FOR EACH ROW EXECUTE FUNCTION public.set_updated_at();


--
-- Name: topic_translations trg_set_updated_at; Type: TRIGGER; Schema: public; Owner: -
--

CREATE TRIGGER trg_set_updated_at BEFORE UPDATE ON public.topic_translations FOR EACH ROW EXECUTE FUNCTION public.set_updated_at();


--
-- Name: topics trg_set_updated_at; Type: TRIGGER; Schema: public; Owner: -
--

CREATE TRIGGER trg_set_updated_at BEFORE UPDATE ON public.topics FOR EACH ROW EXECUTE FUNCTION public.set_updated_at();


--
-- Name: user_misconceptions trg_set_updated_at; Type: TRIGGER; Schema: public; Owner: -
--

CREATE TRIGGER trg_set_updated_at BEFORE UPDATE ON public.user_misconceptions FOR EACH ROW EXECUTE FUNCTION public.set_updated_at();


--
-- Name: users trg_set_updated_at; Type: TRIGGER; Schema: public; Owner: -
--

CREATE TRIGGER trg_set_updated_at BEFORE UPDATE ON public.users FOR EACH ROW EXECUTE FUNCTION public.set_updated_at();


--
-- Name: ai_injection_events ai_injection_events_ai_task_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.ai_injection_events
    ADD CONSTRAINT ai_injection_events_ai_task_id_fkey FOREIGN KEY (ai_task_id) REFERENCES public.ai_tasks(id) ON DELETE SET NULL;


--
-- Name: ai_injection_events ai_injection_events_user_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.ai_injection_events
    ADD CONSTRAINT ai_injection_events_user_id_fkey FOREIGN KEY (user_id) REFERENCES public.users(id) ON DELETE SET NULL;


--
-- Name: ai_tasks ai_tasks_prompt_contract_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.ai_tasks
    ADD CONSTRAINT ai_tasks_prompt_contract_id_fkey FOREIGN KEY (prompt_contract_id) REFERENCES public.prompt_contracts(id) ON DELETE RESTRICT;


--
-- Name: ai_tasks ai_tasks_task_type_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.ai_tasks
    ADD CONSTRAINT ai_tasks_task_type_fkey FOREIGN KEY (task_type) REFERENCES public.ai_task_types(task_type) ON DELETE RESTRICT;


--
-- Name: ai_tasks ai_tasks_user_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.ai_tasks
    ADD CONSTRAINT ai_tasks_user_id_fkey FOREIGN KEY (user_id) REFERENCES public.users(id) ON DELETE SET NULL;


--
-- Name: ai_usage_logs ai_usage_logs_ai_task_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.ai_usage_logs
    ADD CONSTRAINT ai_usage_logs_ai_task_id_fkey FOREIGN KEY (ai_task_id) REFERENCES public.ai_tasks(id) ON DELETE SET NULL;


--
-- Name: ai_usage_logs ai_usage_logs_task_type_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.ai_usage_logs
    ADD CONSTRAINT ai_usage_logs_task_type_fkey FOREIGN KEY (task_type) REFERENCES public.ai_task_types(task_type);


--
-- Name: ai_usage_logs ai_usage_logs_user_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.ai_usage_logs
    ADD CONSTRAINT ai_usage_logs_user_id_fkey FOREIGN KEY (user_id) REFERENCES public.users(id) ON DELETE SET NULL;


--
-- Name: answer_evaluation_misconceptions answer_evaluation_misconceptions_answer_evaluation_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.answer_evaluation_misconceptions
    ADD CONSTRAINT answer_evaluation_misconceptions_answer_evaluation_id_fkey FOREIGN KEY (answer_evaluation_id) REFERENCES public.answer_evaluations(id) ON DELETE CASCADE;


--
-- Name: answer_evaluation_misconceptions answer_evaluation_misconceptions_misconception_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.answer_evaluation_misconceptions
    ADD CONSTRAINT answer_evaluation_misconceptions_misconception_id_fkey FOREIGN KEY (misconception_id) REFERENCES public.misconceptions(id) ON DELETE RESTRICT;


--
-- Name: answer_evaluations answer_evaluations_ai_task_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.answer_evaluations
    ADD CONSTRAINT answer_evaluations_ai_task_id_fkey FOREIGN KEY (ai_task_id) REFERENCES public.ai_tasks(id) ON DELETE SET NULL;


--
-- Name: answer_evaluations answer_evaluations_assessment_answer_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.answer_evaluations
    ADD CONSTRAINT answer_evaluations_assessment_answer_id_fkey FOREIGN KEY (assessment_answer_id) REFERENCES public.assessment_answers(id) ON DELETE CASCADE;


--
-- Name: answer_evaluations answer_evaluations_interview_message_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.answer_evaluations
    ADD CONSTRAINT answer_evaluations_interview_message_id_fkey FOREIGN KEY (interview_message_id) REFERENCES public.interview_messages(id) ON DELETE CASCADE;


--
-- Name: answer_evaluations answer_evaluations_locale_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.answer_evaluations
    ADD CONSTRAINT answer_evaluations_locale_fkey FOREIGN KEY (locale) REFERENCES public.supported_locales(code);


--
-- Name: answer_evaluations answer_evaluations_prompt_contract_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.answer_evaluations
    ADD CONSTRAINT answer_evaluations_prompt_contract_id_fkey FOREIGN KEY (prompt_contract_id) REFERENCES public.prompt_contracts(id) ON DELETE RESTRICT;


--
-- Name: answer_evaluations answer_evaluations_question_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.answer_evaluations
    ADD CONSTRAINT answer_evaluations_question_id_fkey FOREIGN KEY (question_id) REFERENCES public.questions(id) ON DELETE RESTRICT;


--
-- Name: answer_evaluations answer_evaluations_topic_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.answer_evaluations
    ADD CONSTRAINT answer_evaluations_topic_id_fkey FOREIGN KEY (topic_id) REFERENCES public.topics(id) ON DELETE RESTRICT;


--
-- Name: answer_evaluations answer_evaluations_user_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.answer_evaluations
    ADD CONSTRAINT answer_evaluations_user_id_fkey FOREIGN KEY (user_id) REFERENCES public.users(id) ON DELETE RESTRICT;


--
-- Name: app_settings app_settings_updated_by_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.app_settings
    ADD CONSTRAINT app_settings_updated_by_fkey FOREIGN KEY (updated_by) REFERENCES public.users(id) ON DELETE SET NULL;


--
-- Name: assessment_answers assessment_answers_attempt_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.assessment_answers
    ADD CONSTRAINT assessment_answers_attempt_id_fkey FOREIGN KEY (attempt_id) REFERENCES public.assessment_attempts(id) ON DELETE CASCADE;


--
-- Name: assessment_answers assessment_answers_evaluation_prompt_contract_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.assessment_answers
    ADD CONSTRAINT assessment_answers_evaluation_prompt_contract_id_fkey FOREIGN KEY (evaluation_prompt_contract_id) REFERENCES public.prompt_contracts(id) ON DELETE RESTRICT;


--
-- Name: assessment_answers assessment_answers_question_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.assessment_answers
    ADD CONSTRAINT assessment_answers_question_id_fkey FOREIGN KEY (question_id) REFERENCES public.questions(id) ON DELETE RESTRICT;


--
-- Name: assessment_attempts assessment_attempts_locale_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.assessment_attempts
    ADD CONSTRAINT assessment_attempts_locale_fkey FOREIGN KEY (locale) REFERENCES public.supported_locales(code);


--
-- Name: assessment_attempts assessment_attempts_topic_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.assessment_attempts
    ADD CONSTRAINT assessment_attempts_topic_id_fkey FOREIGN KEY (topic_id) REFERENCES public.topics(id) ON DELETE RESTRICT;


--
-- Name: assessment_attempts assessment_attempts_user_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.assessment_attempts
    ADD CONSTRAINT assessment_attempts_user_id_fkey FOREIGN KEY (user_id) REFERENCES public.users(id) ON DELETE RESTRICT;


--
-- Name: concept_translations concept_translations_concept_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.concept_translations
    ADD CONSTRAINT concept_translations_concept_id_fkey FOREIGN KEY (concept_id) REFERENCES public.concepts(id) ON DELETE CASCADE;


--
-- Name: concept_translations concept_translations_locale_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.concept_translations
    ADD CONSTRAINT concept_translations_locale_fkey FOREIGN KEY (locale) REFERENCES public.supported_locales(code) ON DELETE RESTRICT;


--
-- Name: concept_translations concept_translations_reviewer_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.concept_translations
    ADD CONSTRAINT concept_translations_reviewer_id_fkey FOREIGN KEY (reviewer_id) REFERENCES public.users(id) ON DELETE RESTRICT;


--
-- Name: concepts concepts_topic_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.concepts
    ADD CONSTRAINT concepts_topic_id_fkey FOREIGN KEY (topic_id) REFERENCES public.topics(id) ON DELETE RESTRICT;


--
-- Name: golden_cases golden_cases_locale_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.golden_cases
    ADD CONSTRAINT golden_cases_locale_fkey FOREIGN KEY (locale) REFERENCES public.supported_locales(code);


--
-- Name: golden_cases golden_cases_owner_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.golden_cases
    ADD CONSTRAINT golden_cases_owner_id_fkey FOREIGN KEY (owner_id) REFERENCES public.users(id) ON DELETE RESTRICT;


--
-- Name: golden_cases golden_cases_question_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.golden_cases
    ADD CONSTRAINT golden_cases_question_id_fkey FOREIGN KEY (question_id) REFERENCES public.questions(id) ON DELETE RESTRICT;


--
-- Name: golden_recordings golden_recordings_golden_case_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.golden_recordings
    ADD CONSTRAINT golden_recordings_golden_case_id_fkey FOREIGN KEY (golden_case_id) REFERENCES public.golden_cases(id) ON DELETE CASCADE;


--
-- Name: golden_recordings golden_recordings_prompt_contract_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.golden_recordings
    ADD CONSTRAINT golden_recordings_prompt_contract_id_fkey FOREIGN KEY (prompt_contract_id) REFERENCES public.prompt_contracts(id) ON DELETE RESTRICT;


--
-- Name: golden_recordings golden_recordings_source_run_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.golden_recordings
    ADD CONSTRAINT golden_recordings_source_run_id_fkey FOREIGN KEY (source_run_id) REFERENCES public.golden_runs(id) ON DELETE CASCADE;


--
-- Name: golden_run_results golden_run_results_golden_case_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.golden_run_results
    ADD CONSTRAINT golden_run_results_golden_case_id_fkey FOREIGN KEY (golden_case_id) REFERENCES public.golden_cases(id) ON DELETE RESTRICT;


--
-- Name: golden_run_results golden_run_results_golden_run_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.golden_run_results
    ADD CONSTRAINT golden_run_results_golden_run_id_fkey FOREIGN KEY (golden_run_id) REFERENCES public.golden_runs(id) ON DELETE CASCADE;


--
-- Name: golden_runs golden_runs_baseline_run_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.golden_runs
    ADD CONSTRAINT golden_runs_baseline_run_id_fkey FOREIGN KEY (baseline_run_id) REFERENCES public.golden_runs(id) ON DELETE SET NULL;


--
-- Name: golden_runs golden_runs_prompt_contract_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.golden_runs
    ADD CONSTRAINT golden_runs_prompt_contract_id_fkey FOREIGN KEY (prompt_contract_id) REFERENCES public.prompt_contracts(id) ON DELETE RESTRICT;


--
-- Name: golden_runs golden_runs_triggered_by_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.golden_runs
    ADD CONSTRAINT golden_runs_triggered_by_fkey FOREIGN KEY (triggered_by) REFERENCES public.users(id) ON DELETE SET NULL;


--
-- Name: interview_evaluations interview_evaluations_ai_task_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.interview_evaluations
    ADD CONSTRAINT interview_evaluations_ai_task_id_fkey FOREIGN KEY (ai_task_id) REFERENCES public.ai_tasks(id) ON DELETE SET NULL;


--
-- Name: interview_evaluations interview_evaluations_interview_session_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.interview_evaluations
    ADD CONSTRAINT interview_evaluations_interview_session_id_fkey FOREIGN KEY (interview_session_id) REFERENCES public.interview_sessions(id) ON DELETE CASCADE;


--
-- Name: interview_evaluations interview_evaluations_prompt_contract_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.interview_evaluations
    ADD CONSTRAINT interview_evaluations_prompt_contract_id_fkey FOREIGN KEY (prompt_contract_id) REFERENCES public.prompt_contracts(id) ON DELETE RESTRICT;


--
-- Name: interview_messages interview_messages_ai_task_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.interview_messages
    ADD CONSTRAINT interview_messages_ai_task_id_fkey FOREIGN KEY (ai_task_id) REFERENCES public.ai_tasks(id) ON DELETE SET NULL;


--
-- Name: interview_messages interview_messages_evaluation_prompt_contract_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.interview_messages
    ADD CONSTRAINT interview_messages_evaluation_prompt_contract_id_fkey FOREIGN KEY (evaluation_prompt_contract_id) REFERENCES public.prompt_contracts(id) ON DELETE RESTRICT;


--
-- Name: interview_messages interview_messages_interview_session_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.interview_messages
    ADD CONSTRAINT interview_messages_interview_session_id_fkey FOREIGN KEY (interview_session_id) REFERENCES public.interview_sessions(id) ON DELETE CASCADE;


--
-- Name: interview_messages interview_messages_parent_message_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.interview_messages
    ADD CONSTRAINT interview_messages_parent_message_id_fkey FOREIGN KEY (parent_message_id) REFERENCES public.interview_messages(id) ON DELETE RESTRICT;


--
-- Name: interview_messages interview_messages_question_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.interview_messages
    ADD CONSTRAINT interview_messages_question_id_fkey FOREIGN KEY (question_id) REFERENCES public.questions(id) ON DELETE RESTRICT;


--
-- Name: interview_sessions interview_sessions_current_question_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.interview_sessions
    ADD CONSTRAINT interview_sessions_current_question_id_fkey FOREIGN KEY (current_question_id) REFERENCES public.questions(id) ON DELETE RESTRICT;


--
-- Name: interview_sessions interview_sessions_interview_type_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.interview_sessions
    ADD CONSTRAINT interview_sessions_interview_type_fkey FOREIGN KEY (interview_type) REFERENCES public.interview_profiles(interview_type);


--
-- Name: interview_sessions interview_sessions_locale_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.interview_sessions
    ADD CONSTRAINT interview_sessions_locale_fkey FOREIGN KEY (locale) REFERENCES public.supported_locales(code);


--
-- Name: interview_sessions interview_sessions_retry_of_session_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.interview_sessions
    ADD CONSTRAINT interview_sessions_retry_of_session_id_fkey FOREIGN KEY (retry_of_session_id) REFERENCES public.interview_sessions(id) ON DELETE SET NULL;


--
-- Name: interview_sessions interview_sessions_user_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.interview_sessions
    ADD CONSTRAINT interview_sessions_user_id_fkey FOREIGN KEY (user_id) REFERENCES public.users(id) ON DELETE RESTRICT;


--
-- Name: invitations invitations_created_by_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.invitations
    ADD CONSTRAINT invitations_created_by_fkey FOREIGN KEY (created_by) REFERENCES public.users(id) ON DELETE RESTRICT;


--
-- Name: invitations invitations_used_by_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.invitations
    ADD CONSTRAINT invitations_used_by_fkey FOREIGN KEY (used_by) REFERENCES public.users(id) ON DELETE RESTRICT;


--
-- Name: mastery_records mastery_records_last_attempt_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.mastery_records
    ADD CONSTRAINT mastery_records_last_attempt_id_fkey FOREIGN KEY (last_attempt_id) REFERENCES public.topic_attempts(id) ON DELETE SET NULL;


--
-- Name: mastery_records mastery_records_topic_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.mastery_records
    ADD CONSTRAINT mastery_records_topic_id_fkey FOREIGN KEY (topic_id) REFERENCES public.topics(id) ON DELETE RESTRICT;


--
-- Name: mastery_records mastery_records_user_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.mastery_records
    ADD CONSTRAINT mastery_records_user_id_fkey FOREIGN KEY (user_id) REFERENCES public.users(id) ON DELETE RESTRICT;


--
-- Name: material_translations material_translations_locale_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.material_translations
    ADD CONSTRAINT material_translations_locale_fkey FOREIGN KEY (locale) REFERENCES public.supported_locales(code) ON DELETE RESTRICT;


--
-- Name: material_translations material_translations_material_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.material_translations
    ADD CONSTRAINT material_translations_material_id_fkey FOREIGN KEY (material_id) REFERENCES public.materials(id) ON DELETE CASCADE;


--
-- Name: material_translations material_translations_reviewer_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.material_translations
    ADD CONSTRAINT material_translations_reviewer_id_fkey FOREIGN KEY (reviewer_id) REFERENCES public.users(id) ON DELETE RESTRICT;


--
-- Name: materials materials_topic_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.materials
    ADD CONSTRAINT materials_topic_id_fkey FOREIGN KEY (topic_id) REFERENCES public.topics(id) ON DELETE RESTRICT;


--
-- Name: misconception_translations misconception_translations_locale_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.misconception_translations
    ADD CONSTRAINT misconception_translations_locale_fkey FOREIGN KEY (locale) REFERENCES public.supported_locales(code) ON DELETE RESTRICT;


--
-- Name: misconception_translations misconception_translations_misconception_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.misconception_translations
    ADD CONSTRAINT misconception_translations_misconception_id_fkey FOREIGN KEY (misconception_id) REFERENCES public.misconceptions(id) ON DELETE CASCADE;


--
-- Name: misconception_translations misconception_translations_reviewer_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.misconception_translations
    ADD CONSTRAINT misconception_translations_reviewer_id_fkey FOREIGN KEY (reviewer_id) REFERENCES public.users(id) ON DELETE RESTRICT;


--
-- Name: misconceptions misconceptions_topic_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.misconceptions
    ADD CONSTRAINT misconceptions_topic_id_fkey FOREIGN KEY (topic_id) REFERENCES public.topics(id) ON DELETE RESTRICT;


--
-- Name: prompt_contracts prompt_contracts_created_by_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.prompt_contracts
    ADD CONSTRAINT prompt_contracts_created_by_fkey FOREIGN KEY (created_by) REFERENCES public.users(id) ON DELETE SET NULL;


--
-- Name: prompt_contracts prompt_contracts_golden_fk; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.prompt_contracts
    ADD CONSTRAINT prompt_contracts_golden_fk FOREIGN KEY (activated_golden_run_id) REFERENCES public.golden_runs(id) ON DELETE SET NULL;


--
-- Name: prompt_contracts prompt_contracts_task_type_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.prompt_contracts
    ADD CONSTRAINT prompt_contracts_task_type_fkey FOREIGN KEY (task_type) REFERENCES public.ai_task_types(task_type) ON DELETE RESTRICT;


--
-- Name: question_concepts question_concepts_concept_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.question_concepts
    ADD CONSTRAINT question_concepts_concept_id_fkey FOREIGN KEY (concept_id) REFERENCES public.concepts(id) ON DELETE RESTRICT;


--
-- Name: question_concepts question_concepts_question_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.question_concepts
    ADD CONSTRAINT question_concepts_question_id_fkey FOREIGN KEY (question_id) REFERENCES public.questions(id) ON DELETE CASCADE;


--
-- Name: question_translations question_translations_locale_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.question_translations
    ADD CONSTRAINT question_translations_locale_fkey FOREIGN KEY (locale) REFERENCES public.supported_locales(code) ON DELETE RESTRICT;


--
-- Name: question_translations question_translations_question_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.question_translations
    ADD CONSTRAINT question_translations_question_id_fkey FOREIGN KEY (question_id) REFERENCES public.questions(id) ON DELETE CASCADE;


--
-- Name: question_translations question_translations_reviewer_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.question_translations
    ADD CONSTRAINT question_translations_reviewer_id_fkey FOREIGN KEY (reviewer_id) REFERENCES public.users(id) ON DELETE RESTRICT;


--
-- Name: questions questions_topic_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.questions
    ADD CONSTRAINT questions_topic_id_fkey FOREIGN KEY (topic_id) REFERENCES public.topics(id) ON DELETE RESTRICT;


--
-- Name: refresh_tokens refresh_tokens_replaced_by_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.refresh_tokens
    ADD CONSTRAINT refresh_tokens_replaced_by_fkey FOREIGN KEY (replaced_by) REFERENCES public.refresh_tokens(id) ON DELETE SET NULL;


--
-- Name: refresh_tokens refresh_tokens_user_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.refresh_tokens
    ADD CONSTRAINT refresh_tokens_user_id_fkey FOREIGN KEY (user_id) REFERENCES public.users(id) ON DELETE CASCADE;


--
-- Name: roadmap_topics roadmap_topics_roadmap_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.roadmap_topics
    ADD CONSTRAINT roadmap_topics_roadmap_id_fkey FOREIGN KEY (roadmap_id) REFERENCES public.roadmaps(id) ON DELETE CASCADE;


--
-- Name: roadmap_topics roadmap_topics_topic_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.roadmap_topics
    ADD CONSTRAINT roadmap_topics_topic_id_fkey FOREIGN KEY (topic_id) REFERENCES public.topics(id) ON DELETE RESTRICT;


--
-- Name: roadmap_translations roadmap_translations_locale_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.roadmap_translations
    ADD CONSTRAINT roadmap_translations_locale_fkey FOREIGN KEY (locale) REFERENCES public.supported_locales(code) ON DELETE RESTRICT;


--
-- Name: roadmap_translations roadmap_translations_reviewer_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.roadmap_translations
    ADD CONSTRAINT roadmap_translations_reviewer_id_fkey FOREIGN KEY (reviewer_id) REFERENCES public.users(id) ON DELETE RESTRICT;


--
-- Name: roadmap_translations roadmap_translations_roadmap_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.roadmap_translations
    ADD CONSTRAINT roadmap_translations_roadmap_id_fkey FOREIGN KEY (roadmap_id) REFERENCES public.roadmaps(id) ON DELETE CASCADE;


--
-- Name: topic_attempts topic_attempts_assessment_attempt_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.topic_attempts
    ADD CONSTRAINT topic_attempts_assessment_attempt_id_fkey FOREIGN KEY (assessment_attempt_id) REFERENCES public.assessment_attempts(id) ON DELETE RESTRICT;


--
-- Name: topic_attempts topic_attempts_interview_session_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.topic_attempts
    ADD CONSTRAINT topic_attempts_interview_session_id_fkey FOREIGN KEY (interview_session_id) REFERENCES public.interview_sessions(id) ON DELETE RESTRICT;


--
-- Name: topic_attempts topic_attempts_topic_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.topic_attempts
    ADD CONSTRAINT topic_attempts_topic_id_fkey FOREIGN KEY (topic_id) REFERENCES public.topics(id) ON DELETE RESTRICT;


--
-- Name: topic_attempts topic_attempts_user_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.topic_attempts
    ADD CONSTRAINT topic_attempts_user_id_fkey FOREIGN KEY (user_id) REFERENCES public.users(id) ON DELETE RESTRICT;


--
-- Name: topic_prerequisites topic_prerequisites_prerequisite_topic_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.topic_prerequisites
    ADD CONSTRAINT topic_prerequisites_prerequisite_topic_id_fkey FOREIGN KEY (prerequisite_topic_id) REFERENCES public.topics(id) ON DELETE CASCADE;


--
-- Name: topic_prerequisites topic_prerequisites_topic_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.topic_prerequisites
    ADD CONSTRAINT topic_prerequisites_topic_id_fkey FOREIGN KEY (topic_id) REFERENCES public.topics(id) ON DELETE CASCADE;


--
-- Name: topic_translations topic_translations_locale_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.topic_translations
    ADD CONSTRAINT topic_translations_locale_fkey FOREIGN KEY (locale) REFERENCES public.supported_locales(code) ON DELETE RESTRICT;


--
-- Name: topic_translations topic_translations_reviewer_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.topic_translations
    ADD CONSTRAINT topic_translations_reviewer_id_fkey FOREIGN KEY (reviewer_id) REFERENCES public.users(id) ON DELETE RESTRICT;


--
-- Name: topic_translations topic_translations_topic_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.topic_translations
    ADD CONSTRAINT topic_translations_topic_id_fkey FOREIGN KEY (topic_id) REFERENCES public.topics(id) ON DELETE CASCADE;


--
-- Name: user_misconceptions user_misconceptions_misconception_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.user_misconceptions
    ADD CONSTRAINT user_misconceptions_misconception_id_fkey FOREIGN KEY (misconception_id) REFERENCES public.misconceptions(id) ON DELETE RESTRICT;


--
-- Name: user_misconceptions user_misconceptions_resolved_by_evaluation_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.user_misconceptions
    ADD CONSTRAINT user_misconceptions_resolved_by_evaluation_id_fkey FOREIGN KEY (resolved_by_evaluation_id) REFERENCES public.answer_evaluations(id) ON DELETE SET NULL;


--
-- Name: user_misconceptions user_misconceptions_resolved_by_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.user_misconceptions
    ADD CONSTRAINT user_misconceptions_resolved_by_fkey FOREIGN KEY (resolved_by) REFERENCES public.users(id) ON DELETE SET NULL;


--
-- Name: user_misconceptions user_misconceptions_source_evaluation_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.user_misconceptions
    ADD CONSTRAINT user_misconceptions_source_evaluation_id_fkey FOREIGN KEY (source_evaluation_id) REFERENCES public.answer_evaluations(id) ON DELETE SET NULL;


--
-- Name: user_misconceptions user_misconceptions_user_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.user_misconceptions
    ADD CONSTRAINT user_misconceptions_user_id_fkey FOREIGN KEY (user_id) REFERENCES public.users(id) ON DELETE RESTRICT;


--
-- Name: user_roadmaps user_roadmaps_roadmap_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.user_roadmaps
    ADD CONSTRAINT user_roadmaps_roadmap_id_fkey FOREIGN KEY (roadmap_id) REFERENCES public.roadmaps(id) ON DELETE CASCADE;


--
-- Name: user_roadmaps user_roadmaps_user_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.user_roadmaps
    ADD CONSTRAINT user_roadmaps_user_id_fkey FOREIGN KEY (user_id) REFERENCES public.users(id) ON DELETE CASCADE;


--
-- Name: users users_preferred_locale_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.users
    ADD CONSTRAINT users_preferred_locale_fkey FOREIGN KEY (preferred_locale) REFERENCES public.supported_locales(code) ON DELETE RESTRICT;


--
-- PostgreSQL database dump complete
--


