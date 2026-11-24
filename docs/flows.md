# How Things Flow

This document describes the main user journeys. It is written for humans, not as an API reference — see the README for setup and endpoint lists.

## The big picture

```
                    ┌──────────────┐
   Learner / Admin ─▶│  API /api/v1 │──▶ PostgreSQL
                    └──────┬───────┘
                           │ enqueue
                           ▼
                      ai_tasks (queue)
                           │
                           ▼
                     Worker ──▶ AI Provider
                           │
                           ▼
              server-side scoring + mastery
```

AI never decides the final score or mastery status. It only returns signals (concept coverage, misconception codes, dimension scores). The server computes everything else.

---

## Case 1 — Learner wants to learn a topic

```
GET /topics?locale=id          → topics that have an Indonesian translation published
GET /topics/{id}               → topic detail
GET /topics/{id}/materials     → study materials
GET /me/progress/topics/{tid}  → current mastery (NOT_STARTED if new)
GET /me/recommendations        → what to study next
```

Recommendations follow a fixed priority:
1. a prerequisite that is not mastered yet,
2. low mastery / `NEEDS_REVIEW`,
3. relevant to a roadmap the learner selected,
4. normal curriculum order.

A learner can also pick a roadmap with `PUT /me/roadmaps/{roadmap_id}`.

---

## Case 2 — Learner takes an assessment

```
1. POST /assessments/attempts {topic_id, locale}
        → attempt is created with published questions (at least one application question)

2. PUT /assessments/attempts/{id}/answers/{question_id} {answer_text}
        → answers are saved as drafts

3. POST /assessments/attempts/{id}/submit
        → blank answer   : scored immediately with the minimum score, no AI
        → written answer : pending, one AI task is enqueued
        → attempt status : submitted

4. Worker picks up the AI task and calls the Evaluator
        → answer_evaluation row saved
        → answer becomes scored
        → attempt is finalized:
             score = weighted average of SCORED answers only
             unscored answers are ignored
             topic_attempts + user_misconceptions + mastery_records are updated

5. GET /assessments/attempts/{id} → result, with evaluation_status per answer
```

Important rules:
- `pending` or `failed` answers hold back finalization (attempt stays `submitted`).
- If every written answer is `unscored`, the attempt becomes `unscored` and **no** `topic_attempts` or mastery change happens.
- A failed evaluation can be retried: `POST .../answers/{question_id}/evaluation/retry`. It returns `202` (new task), `200` (already pending — idempotent), or `409` (not retryable).
- A failed answer older than the retry window (24h by default) becomes `unscored` automatically.

---

## Case 3 — Learner does an interview

```
1. GET /interview-profiles                 → practice or recruitment limits
2. POST /interviews {interview_type, topic_ids, locale}
        → profile settings are snapshotted into configuration
        → active prompt-contract versions are pinned to the session

3. POST /interviews/{id}/start             → IN_PROGRESS, first question message written
4. GET  /interviews/{id}/messages          → transcript so far
5. POST /interviews/{id}/answers {question_id, content}
        → answer message stored as pending, one priority-1 AI task enqueued

6. Worker evaluates the answer:
        → result saved, answer becomes scored
        → follow-up allocation decides:
             ask a follow-up  → follow-up task + follow-up message
             next main question → next question message
             no questions left → finalize

7. Pause / resume:
        POST /interviews/{id}/pause   → PAUSED, expires in 7 days
        POST /interviews/{id}/resume  → back to IN_PROGRESS (409 if expired)

8. POST /interviews/{id}/finish
        → COMPLETED immediately (does not wait for the dimension evaluation)
        → interview_evaluation task enqueued
        → Attempts recorded per topic from scored answers

9. GET /interviews/{id}/evaluation
        → overall_score, five dimension scores, per-topic scores,
          and the latest mastery status of each topic
```

Notes:
- `overall_score` is an interview dimension result. It is **never** used as an Attempt score or Mastery input.
- A failed answer does not produce a follow-up; the interview simply moves to the next main question.
- Answers in an abandoned session become `unscored`, and the result of a late AI task is discarded.
- Answer retries: `POST /interviews/{id}/messages/{message_id}/evaluation/retry`.
- Dimension-evaluation retry: `POST /interviews/{id}/evaluation/retry`.

---

## Case 4 — Admin publishes content

```
Create entity      POST /admin/{topics|materials|concepts|questions|roadmaps|misconceptions}
Add translation    PUT  /admin/{resource}/{id}/translations/{locale}   → draft, version 1
Edit translation   PUT  ... (with version)  → version + 1
                                              editing a published one returns it to draft
Change status      POST .../translations/{locale}/transitions
                     draft → in_review → published → archived → draft
```

A translation is visible to learners only when its status is `published` on their requested locale. Publishing requires an admin reviewer; publishing a question also requires at least one concept and a non-empty reference answer.

Related admin actions: misconception catalog (`severity`, disable), topic prerequisites (cycle-free DAG), question–concept weights, and roadmap topic order.

---

## Case 5 — Admin changes the AI evaluator

```
1. Create golden cases (expected score range, concepts, misconception codes)
2. Run them:
      POST /admin/golden-runs {run_mode: "recorded"}  → CI, replayed responses, no model calls
      POST /admin/golden-runs {run_mode: "live"}      → real model, manual or scheduled
3. Inspect metrics (score-in-range, concept F1, critical recall, etc.)
4. Activate a new answer_evaluation prompt version:
      POST /admin/prompt-contracts/{id}/activate
      → allowed only with a passing LIVE golden run (or a recorded waiver reason)
      → old active version is retired in the same transaction
```

Recorded runs are for regression checks in CI. Only live runs can become the baseline or satisfy the activation gate.

---

## Failure and retry summary

| Situation | Result |
|---|---|
| AI provider down / invalid output | AITask fails; answer becomes `failed`, then `unscored` after retries or timeout |
| Learner retries a failed answer | New AITask with the same pinned prompt version; idempotent under concurrency |
| Two concurrent refresh requests | Whole token family revoked — clients must use single-flight |
| Missing translation | `404 translation_not_available` with the list of available locales |
| Invalid state transition | `409` with a specific error code |
