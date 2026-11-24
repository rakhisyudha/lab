ALTER TABLE prompt_contracts DROP CONSTRAINT IF EXISTS prompt_contracts_golden_fk;
DROP TABLE IF EXISTS golden_recordings;
DROP TABLE IF EXISTS golden_run_results;
DROP TABLE IF EXISTS golden_cases;
DROP TABLE IF EXISTS golden_runs;
