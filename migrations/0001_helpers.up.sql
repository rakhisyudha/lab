CREATE OR REPLACE FUNCTION set_updated_at() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
  NEW.updated_at := now();
  RETURN NEW;
END $$;

CREATE OR REPLACE FUNCTION attach_updated_at_trigger(tbl regclass) RETURNS void
LANGUAGE plpgsql AS $$
BEGIN
  EXECUTE format(
    'CREATE TRIGGER trg_set_updated_at BEFORE UPDATE ON %s
       FOR EACH ROW EXECUTE FUNCTION set_updated_at()', tbl);
END $$;

-- Reviewer pada Translation harus user ber-role admin (Requirement 4.4, Keputusan D-4)
CREATE OR REPLACE FUNCTION enforce_reviewer_admin() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
  IF NEW.reviewer_id IS NOT NULL AND NOT EXISTS (
       SELECT 1 FROM users u WHERE u.id = NEW.reviewer_id AND u.role = 'admin') THEN
    RAISE EXCEPTION 'reviewer_must_be_admin' USING ERRCODE = '23514';
  END IF;
  RETURN NEW;
END $$;

CREATE OR REPLACE FUNCTION attach_translation_triggers(tbl regclass) RETURNS void
LANGUAGE plpgsql AS $$
BEGIN
  PERFORM attach_updated_at_trigger(tbl);
  EXECUTE format(
    'CREATE TRIGGER trg_reviewer_admin BEFORE INSERT OR UPDATE OF reviewer_id ON %s
       FOR EACH ROW EXECUTE FUNCTION enforce_reviewer_admin()', tbl);
END $$;
