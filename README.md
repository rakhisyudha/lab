# Backend Lab

A Go backend for learning: bilingual content, assessments, AI interviews, automated grading, and Mastery v1.

## Architecture at a Glance

```
Client → API (Gin, /api/v1) → PostgreSQL
              ↓
         ai_tasks (queue) → Worker → AI Provider
```

- **API** (`cmd/api`) serves HTTP requests.
- **Worker** (`cmd/worker`) processes the AI task queue and scheduled jobs.
- **PostgreSQL** is the only datastore. Queue, quotas, and rate-limit state live in tables, not Redis.

Grading logic lives in `internal/evaluation` as **pure functions** (no database, clock, or randomness). The server computes final scores and mastery status; AI only provides signals.

**New here?** Read [docs/flows.md](docs/flows.md) for end-to-end journeys — learning a topic, taking an assessment, doing an interview, publishing content, and validating an AI prompt change.

## Getting Started

Requires Go 1.26+ and PostgreSQL 13+.

```bash
# 1. Configure
cp .env.example .env
# Edit .env: set DATABASE_URL, JWT_SIGNING_KEY, and the admin password.

# 2. Migrate and seed
go run ./cmd/migrate up
go run ./cmd/migrate seed-prompts
go run ./cmd/migrate bootstrap-admin --email=admin@example.id

# 3. Start the API (default port 8080; override with PORT)
go run ./cmd/api

# 4. Start the worker in another terminal
go run ./cmd/worker
```

All commands load `.env` automatically. Variables exported in your shell (or by CI/containers) override the file.
The password for `bootstrap-admin` is read from `BOOTSTRAP_ADMIN_PASSWORD`.

See `.env.example` for every supported variable. No credentials are hardcoded in the code.

## Main Endpoints

| Area | Example |
|---|---|
| Auth | `/api/v1/auth/register`, `/login`, `/refresh`, `/logout`, `/me` |
| Content | `/api/v1/topics`, `/topics/{id}/materials`, `/materials/{id}`, `/roadmaps` |
| Assessment | `/api/v1/assessments/attempts`, `.../submit`, `.../evaluation/retry` |
| Interview | `/api/v1/interviews`, `.../start`, `.../pause`, `.../resume`, `.../answers`, `.../evaluation` |
| Progress | `/api/v1/me/progress`, `/me/misconceptions`, `/me/recommendations` |
| Admin | `/api/v1/admin/{topics,materials,questions,...}`, `/settings`, `/golden-runs` |
| Health | `/api/v1/health` |

Every error uses one shape: `{"error":{"code","message","details","request_id"}}`.

## Testing

```bash
go test ./...
bash scripts/check-properties.sh          # vet + gofmt + tests + mandatory-property check
go test -rapid.checks=100 -run '^TestPropW' ./internal/...   # mandatory property tests
bash scripts/verify-migrations.sh          # needs TEST_DATABASE_URL
```

The mandatory property tests cover scoring, mastery, state machines, the task queue, and token invariants.

## Behaviour Worth Knowing

- **No refresh-token grace window** — two concurrent refreshes revoke the whole session. Clients must use single-flight (example in `examples/web-client`).
- **Blank answers** score the minimum value immediately, without calling AI.
- **Mastery** is computed only from `scored` answers; a failed evaluation never changes mastery.
- **Golden Set `recorded` mode** runs in CI with replayed responses and no AI provider. `live` mode calls a real model and runs manually or on a schedule.
- Tunable parameters (thresholds, quotas, TTLs) live in `app_settings` and can be changed by an admin without a redeploy.

## Layout

```
cmd/api, cmd/worker, cmd/migrate   binaries
internal/platform                  config, db, HTTP, pagination, settings, ratelimit
internal/{auth,content,assessment,interview,progress,recommendation}  business modules
internal/ai                        provider, gateway, guard, prompt, queue, quota
internal/evaluation                pure functions + property tests
internal/golden, internal/admin    Golden Set and AI configuration
migrations, prompts, test, scripts SQL, prompt contracts, fixtures, CI scripts
```

Full specifications are in `.kiro/specs/backend-lab-technical-specs/`.
