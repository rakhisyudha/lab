-- ===== Topic =====
CREATE TABLE topics (
  id         uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  slug       text NOT NULL CHECK (slug ~ '^[a-z0-9]+(-[a-z0-9]+)*$'),
  position   integer NOT NULL CHECK (position >= 0),     -- urutan kurikulum
  difficulty text NOT NULL DEFAULT 'beginner' CHECK (difficulty IN ('beginner','intermediate','advanced')),
  is_active  boolean NOT NULL DEFAULT true,
  deleted_at timestamptz,
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now()
);
CREATE UNIQUE INDEX topics_slug_uk      ON topics (slug) WHERE deleted_at IS NULL;
CREATE INDEX        topics_position_idx ON topics (position, id) WHERE deleted_at IS NULL AND is_active;
SELECT attach_updated_at_trigger('topics');

CREATE TABLE topic_translations (
  id           uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  topic_id     uuid NOT NULL REFERENCES topics(id) ON DELETE CASCADE,
  locale       text NOT NULL REFERENCES supported_locales(code) ON DELETE RESTRICT,
  title        text NOT NULL CHECK (char_length(title) BETWEEN 1 AND 200),
  summary      text NOT NULL DEFAULT '',
  status       text NOT NULL DEFAULT 'draft' CHECK (status IN ('draft','in_review','published','archived')),
  version      integer NOT NULL DEFAULT 1 CHECK (version >= 1),
  reviewer_id  uuid REFERENCES users(id) ON DELETE RESTRICT,
  reviewed_at  timestamptz,
  published_at timestamptz,
  created_at   timestamptz NOT NULL DEFAULT now(),
  updated_at   timestamptz NOT NULL DEFAULT now(),
  CONSTRAINT topic_translations_locale_uk UNIQUE (topic_id, locale),
  CONSTRAINT topic_translations_pub_chk CHECK (status <> 'published'
      OR (reviewer_id IS NOT NULL AND reviewed_at IS NOT NULL AND published_at IS NOT NULL))
);
-- hot path serving dan penegasan Requirement 3.8
CREATE UNIQUE INDEX topic_translations_published_uk ON topic_translations (topic_id, locale) WHERE status = 'published';
SELECT attach_translation_triggers('topic_translations');

-- ===== Prasyarat Topic (DAG; siklus dicegah di service) =====
CREATE TABLE topic_prerequisites (
  topic_id              uuid NOT NULL REFERENCES topics(id) ON DELETE CASCADE,
  prerequisite_topic_id uuid NOT NULL REFERENCES topics(id) ON DELETE CASCADE,
  created_at            timestamptz NOT NULL DEFAULT now(),
  PRIMARY KEY (topic_id, prerequisite_topic_id),
  CHECK (topic_id <> prerequisite_topic_id)
);
CREATE INDEX topic_prerequisites_prereq_idx ON topic_prerequisites (prerequisite_topic_id);

-- ===== Material =====
CREATE TABLE materials (
  id                uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  topic_id          uuid NOT NULL REFERENCES topics(id) ON DELETE RESTRICT,
  slug              text NOT NULL CHECK (slug ~ '^[a-z0-9]+(-[a-z0-9]+)*$'),
  position          integer NOT NULL CHECK (position >= 0),
  estimated_minutes integer CHECK (estimated_minutes > 0),
  is_active         boolean NOT NULL DEFAULT true,
  deleted_at        timestamptz,
  created_at        timestamptz NOT NULL DEFAULT now(),
  updated_at        timestamptz NOT NULL DEFAULT now()
);
CREATE UNIQUE INDEX materials_topic_slug_uk ON materials (topic_id, slug) WHERE deleted_at IS NULL;
CREATE INDEX materials_topic_pos_idx ON materials (topic_id, position, id) WHERE deleted_at IS NULL AND is_active;
-- indeks penuh untuk FK: pemeriksaan ON DELETE RESTRICT tidak memakai partial index
CREATE INDEX materials_topic_id_idx ON materials (topic_id);
SELECT attach_updated_at_trigger('materials');

CREATE TABLE material_translations (
  id           uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  material_id  uuid NOT NULL REFERENCES materials(id) ON DELETE CASCADE,
  locale       text NOT NULL REFERENCES supported_locales(code) ON DELETE RESTRICT,
  title        text NOT NULL CHECK (char_length(title) BETWEEN 1 AND 200),
  summary      text NOT NULL DEFAULT '',
  body_md      text NOT NULL DEFAULT '',
  status       text NOT NULL DEFAULT 'draft' CHECK (status IN ('draft','in_review','published','archived')),
  version      integer NOT NULL DEFAULT 1 CHECK (version >= 1),
  reviewer_id  uuid REFERENCES users(id) ON DELETE RESTRICT,
  reviewed_at  timestamptz,
  published_at timestamptz,
  created_at   timestamptz NOT NULL DEFAULT now(),
  updated_at   timestamptz NOT NULL DEFAULT now(),
  CONSTRAINT material_translations_locale_uk UNIQUE (material_id, locale),
  CONSTRAINT material_translations_pub_chk CHECK (status <> 'published'
      OR (reviewer_id IS NOT NULL AND reviewed_at IS NOT NULL AND published_at IS NOT NULL))
);
CREATE UNIQUE INDEX material_translations_published_uk ON material_translations (material_id, locale) WHERE status = 'published';
SELECT attach_translation_triggers('material_translations');

-- ===== Concept (unit pengetahuan yang dinilai) =====
CREATE TABLE concepts (
  id         uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  topic_id   uuid NOT NULL REFERENCES topics(id) ON DELETE RESTRICT,
  code       text NOT NULL CHECK (code ~ '^[a-z][a-z0-9_]*$'),
  is_active  boolean NOT NULL DEFAULT true,
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now(),
  CONSTRAINT concepts_topic_code_uk UNIQUE (topic_id, code)
);
SELECT attach_updated_at_trigger('concepts');

CREATE TABLE concept_translations (
  id           uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  concept_id   uuid NOT NULL REFERENCES concepts(id) ON DELETE CASCADE,
  locale       text NOT NULL REFERENCES supported_locales(code) ON DELETE RESTRICT,
  name         text NOT NULL CHECK (char_length(name) BETWEEN 1 AND 200),
  description  text NOT NULL DEFAULT '',
  status       text NOT NULL DEFAULT 'draft' CHECK (status IN ('draft','in_review','published','archived')),
  version      integer NOT NULL DEFAULT 1 CHECK (version >= 1),
  reviewer_id  uuid REFERENCES users(id) ON DELETE RESTRICT,
  reviewed_at  timestamptz,
  published_at timestamptz,
  created_at   timestamptz NOT NULL DEFAULT now(),
  updated_at   timestamptz NOT NULL DEFAULT now(),
  CONSTRAINT concept_translations_locale_uk UNIQUE (concept_id, locale),
  CONSTRAINT concept_translations_pub_chk CHECK (status <> 'published'
      OR (reviewer_id IS NOT NULL AND reviewed_at IS NOT NULL AND published_at IS NOT NULL))
);
CREATE UNIQUE INDEX concept_translations_published_uk ON concept_translations (concept_id, locale) WHERE status = 'published';
SELECT attach_translation_triggers('concept_translations');

-- ===== Misconception catalog (Keputusan D-1) =====
CREATE TABLE misconceptions (
  id         uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  topic_id   uuid NOT NULL REFERENCES topics(id) ON DELETE RESTRICT,
  code       text NOT NULL CHECK (code ~ '^([a-z][a-z0-9_]*|[A-Z][A-Z0-9_]*)$'),
  severity   text NOT NULL CHECK (severity IN ('minor','critical')),
  is_active  boolean NOT NULL DEFAULT true,
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now(),
  CONSTRAINT misconceptions_topic_code_uk UNIQUE (topic_id, code)
);
CREATE INDEX misconceptions_topic_active_idx ON misconceptions (topic_id) WHERE is_active;
SELECT attach_updated_at_trigger('misconceptions');

-- code dan topic_id tidak berubah setelah dibuat (Requirement 6.5)
CREATE OR REPLACE FUNCTION misconceptions_immutable() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
  IF NEW.code <> OLD.code OR NEW.topic_id <> OLD.topic_id THEN
    RAISE EXCEPTION 'misconception_code_immutable' USING ERRCODE = '23514';
  END IF;
  RETURN NEW;
END $$;
CREATE TRIGGER trg_misconceptions_immutable BEFORE UPDATE ON misconceptions
  FOR EACH ROW EXECUTE FUNCTION misconceptions_immutable();

CREATE TABLE misconception_translations (
  id               uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  misconception_id uuid NOT NULL REFERENCES misconceptions(id) ON DELETE CASCADE,
  locale           text NOT NULL REFERENCES supported_locales(code) ON DELETE RESTRICT,
  title            text NOT NULL CHECK (char_length(title) BETWEEN 1 AND 200),
  description      text NOT NULL DEFAULT '',
  status           text NOT NULL DEFAULT 'draft' CHECK (status IN ('draft','in_review','published','archived')),
  version          integer NOT NULL DEFAULT 1 CHECK (version >= 1),
  reviewer_id      uuid REFERENCES users(id) ON DELETE RESTRICT,
  reviewed_at      timestamptz,
  published_at     timestamptz,
  created_at       timestamptz NOT NULL DEFAULT now(),
  updated_at       timestamptz NOT NULL DEFAULT now(),
  CONSTRAINT misconception_translations_locale_uk UNIQUE (misconception_id, locale),
  CONSTRAINT misconception_translations_pub_chk CHECK (status <> 'published'
      OR (reviewer_id IS NOT NULL AND reviewed_at IS NOT NULL AND published_at IS NOT NULL))
);
CREATE UNIQUE INDEX misconception_translations_published_uk ON misconception_translations (misconception_id, locale) WHERE status = 'published';
SELECT attach_translation_triggers('misconception_translations');

-- ===== Question bank =====
CREATE TABLE questions (
  id              uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  topic_id        uuid NOT NULL REFERENCES topics(id) ON DELETE RESTRICT,
  cognitive_level text NOT NULL CHECK (cognitive_level IN ('recall','conceptual','application','scenario')),
  difficulty      smallint NOT NULL DEFAULT 1 CHECK (difficulty BETWEEN 1 AND 3),
  purpose         text NOT NULL DEFAULT 'both' CHECK (purpose IN ('assessment','interview','both')),
  weight          numeric(5,2) NOT NULL DEFAULT 1 CHECK (weight > 0),
  is_active       boolean NOT NULL DEFAULT true,
  deleted_at      timestamptz,
  created_at      timestamptz NOT NULL DEFAULT now(),
  updated_at      timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX questions_topic_idx ON questions (topic_id, cognitive_level) WHERE deleted_at IS NULL AND is_active;
-- indeks penuh untuk FK: pemeriksaan ON DELETE RESTRICT tidak memakai partial index
CREATE INDEX questions_topic_id_idx ON questions (topic_id);
SELECT attach_updated_at_trigger('questions');

CREATE TABLE question_translations (
  id               uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  question_id      uuid NOT NULL REFERENCES questions(id) ON DELETE CASCADE,
  locale           text NOT NULL REFERENCES supported_locales(code) ON DELETE RESTRICT,
  prompt           text NOT NULL CHECK (char_length(prompt) BETWEEN 1 AND 4000),
  reference_answer text NOT NULL DEFAULT '',
  evaluation_notes text NOT NULL DEFAULT '',
  status           text NOT NULL DEFAULT 'draft' CHECK (status IN ('draft','in_review','published','archived')),
  version          integer NOT NULL DEFAULT 1 CHECK (version >= 1),
  reviewer_id      uuid REFERENCES users(id) ON DELETE RESTRICT,
  reviewed_at      timestamptz,
  published_at     timestamptz,
  created_at       timestamptz NOT NULL DEFAULT now(),
  updated_at       timestamptz NOT NULL DEFAULT now(),
  CONSTRAINT question_translations_locale_uk UNIQUE (question_id, locale),
  CONSTRAINT question_translations_pub_chk CHECK (status <> 'published'
      OR (reviewer_id IS NOT NULL AND reviewed_at IS NOT NULL AND published_at IS NOT NULL
          AND char_length(reference_answer) > 0))
);
CREATE UNIQUE INDEX question_translations_published_uk ON question_translations (question_id, locale) WHERE status = 'published';
SELECT attach_translation_triggers('question_translations');

CREATE TABLE question_concepts (
  question_id uuid NOT NULL REFERENCES questions(id) ON DELETE CASCADE,
  concept_id  uuid NOT NULL REFERENCES concepts(id) ON DELETE RESTRICT,
  weight      numeric(5,2) NOT NULL DEFAULT 1 CHECK (weight > 0),
  is_required boolean NOT NULL DEFAULT false,
  created_at  timestamptz NOT NULL DEFAULT now(),
  updated_at  timestamptz NOT NULL DEFAULT now(),
  PRIMARY KEY (question_id, concept_id)
);
CREATE INDEX question_concepts_concept_idx ON question_concepts (concept_id);
SELECT attach_updated_at_trigger('question_concepts');
-- Service: concept.topic_id harus sama dengan question.topic_id; publish ditolak bila question tanpa concept (Requirement 20.4)

-- ===== Roadmap =====
CREATE TABLE roadmaps (
  id         uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  slug       text NOT NULL CHECK (slug ~ '^[a-z0-9]+(-[a-z0-9]+)*$'),
  is_active  boolean NOT NULL DEFAULT true,
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now(),
  CONSTRAINT roadmaps_slug_uk UNIQUE (slug)
);
SELECT attach_updated_at_trigger('roadmaps');

CREATE TABLE roadmap_translations (
  id           uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  roadmap_id   uuid NOT NULL REFERENCES roadmaps(id) ON DELETE CASCADE,
  locale       text NOT NULL REFERENCES supported_locales(code) ON DELETE RESTRICT,
  title        text NOT NULL CHECK (char_length(title) BETWEEN 1 AND 200),
  description  text NOT NULL DEFAULT '',
  status       text NOT NULL DEFAULT 'draft' CHECK (status IN ('draft','in_review','published','archived')),
  version      integer NOT NULL DEFAULT 1 CHECK (version >= 1),
  reviewer_id  uuid REFERENCES users(id) ON DELETE RESTRICT,
  reviewed_at  timestamptz,
  published_at timestamptz,
  created_at   timestamptz NOT NULL DEFAULT now(),
  updated_at   timestamptz NOT NULL DEFAULT now(),
  CONSTRAINT roadmap_translations_locale_uk UNIQUE (roadmap_id, locale),
  CONSTRAINT roadmap_translations_pub_chk CHECK (status <> 'published'
      OR (reviewer_id IS NOT NULL AND reviewed_at IS NOT NULL AND published_at IS NOT NULL))
);
CREATE UNIQUE INDEX roadmap_translations_published_uk ON roadmap_translations (roadmap_id, locale) WHERE status = 'published';
SELECT attach_translation_triggers('roadmap_translations');

CREATE TABLE roadmap_topics (
  roadmap_id uuid NOT NULL REFERENCES roadmaps(id) ON DELETE CASCADE,
  topic_id   uuid NOT NULL REFERENCES topics(id) ON DELETE RESTRICT,
  position   integer NOT NULL CHECK (position >= 0),
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now(),
  PRIMARY KEY (roadmap_id, topic_id),
  CONSTRAINT roadmap_topics_pos_uk UNIQUE (roadmap_id, position) DEFERRABLE INITIALLY DEFERRED
);
CREATE INDEX roadmap_topics_topic_idx ON roadmap_topics (topic_id);
SELECT attach_updated_at_trigger('roadmap_topics');

CREATE TABLE user_roadmaps (
  user_id     uuid NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  roadmap_id  uuid NOT NULL REFERENCES roadmaps(id) ON DELETE CASCADE,
  selected_at timestamptz NOT NULL DEFAULT now(),
  PRIMARY KEY (user_id, roadmap_id)
);
CREATE INDEX user_roadmaps_roadmap_idx ON user_roadmaps (roadmap_id);
