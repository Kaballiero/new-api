# review.md — Code Review Checklist for new-api

Project-specific review checklist. Use alongside (not instead of) general "does it work, is it readable, is it tested" review.

Cite findings as `file_path:line_number`. Quote error strings exactly.

---

## 1. JSON handling (Rule 1)

- [ ] No `import "encoding/json"` in business code for `Marshal` / `Unmarshal` / `NewDecoder(...).Decode` / `NewEncoder(...).Encode`. Must go through `common.Marshal` / `common.Unmarshal` / `common.UnmarshalJsonStr` / `common.DecodeJson`.
- [ ] `json.RawMessage` / `json.Number` may still be referenced as **types** — that's fine.
- [ ] No `json.Marshal(...)` / `json.Unmarshal(...)` sneaking in via copy-paste from external samples.

Grep: `\bjson\.(Marshal|Unmarshal|NewEncoder|NewDecoder)\b` in `controller/`, `service/`, `relay/`, `dto/`, `model/`.

---

## 2. Cross-DB compatibility (Rule 2)

- [ ] No `AUTO_INCREMENT`, `SERIAL`, `SERIAL PRIMARY KEY` written by hand — GORM handles PKs.
- [ ] No raw `\`group\`` / `\`key\`` / `"group"` / `"key"` — use `commonGroupCol`, `commonKeyCol`.
- [ ] No literal `= 1` / `= 0` / `= true` / `= false` in raw SQL boolean predicates — use `commonTrueVal`, `commonFalseVal`.
- [ ] No `GROUP_CONCAT` without PG `STRING_AGG` branch (gated by `common.UsingPostgreSQL`).
- [ ] No JSONB operators (`@>`, `?`, `?|`, `?&`, `->`, `->>`) unguarded.
- [ ] No `ALTER COLUMN` in any migration touching SQLite — use `ALTER TABLE … ADD COLUMN` pattern from `model/main.go`.
- [ ] No DB-specific column type literals (`JSONB`, `MEDIUMTEXT`, `BIGSERIAL`, `TIMESTAMPTZ`) without fallback.
- [ ] If branching on DB engine: cover all three (`UsingMySQL`, `UsingPostgreSQL`, `UsingSQLite`) — don't leave SQLite to "default" fallthrough if the branch is non-trivial.
- [ ] New migration: state how it was tested on SQLite, MySQL, PG (or note "GORM auto-migrate, no manual SQL").

---

## 3. Relay DTO pointer semantics (Rule 6)

- [ ] Optional scalar fields on request DTOs are `*int` / `*uint` / `*float64` / `*bool` — NOT `int + omitempty`.
- [ ] Zero values from client must propagate upstream: `temperature: 0`, `top_p: 0`, `stream: false`, `frequency_penalty: 0`, `presence_penalty: 0`, `n: 0`, `max_tokens: 0` (where applicable).
- [ ] If new optional field added: add a guard test in `dto/openai_request_zero_value_test.go` (or equivalent for claude/gemini).
- [ ] Re-marshal path: when convert layer copies fields, it must dereference pointers correctly (no implicit `0` substitution).

Grep: `\bomitempty\b` near `int |float64 |bool ` in `dto/*.go`.

---

## 4. New channel adapter (Rule 4)

- [ ] Channel directory under `relay/channel/<name>/` with: `adaptor.go`, `relay-<name>.go`, `dto.go` (or shared dto), `constants.go`.
- [ ] `Adaptor` interface fully implemented (all `Convert*` methods present; return `nil, errors.New("not supported")` for unsupported modes — do not silently no-op).
- [ ] API type constant added in `constant/api_type.go`.
- [ ] Channel type constant added in `constant/channel.go` (with default base URL entry).
- [ ] `GetAdaptor()` switch in `relay/relay_adaptor.go` wired.
- [ ] If provider supports stream usage: channel added to `streamSupportedChannels`.
- [ ] Auth header style matches provider (Bearer vs api-key vs custom signature). For SigV4 / HMAC, ensure body is read once (or re-buffered) and content-hash is computed correctly.
- [ ] Error handling: wrap upstream errors via `types.NewAPIError` rather than raw `error`. Preserve status code mapping.
- [ ] Token counting: `service.CountTokenInput / CountTokenMessages / CountAudioToken` used where relevant; do not invent ad-hoc counters.
- [ ] Streaming: SSE framing handled, `data: [DONE]` honored, `done` channel closed once, no goroutine leaks on client disconnect.
- [ ] Tests: at minimum a non-stream and stream happy-path test, plus an upstream-error mapping test.

---

## 5. Billing / quota / expression system (Rule 7)

- [ ] Read `pkg/billingexpr/expr.md` if touching expression compile/eval/settle.
- [ ] Use `len` (not `p`) for tier conditions in expressions to avoid cache-hit drift.
- [ ] `p`/`c` auto-exclusion logic preserved: when adding new token category, AST introspection must detect it and adjust normalization.
- [ ] Pre-consume vs settlement parity: settlement formula = pre-consume formula at the same expression version.
- [ ] Quota changes go through `service/quota.go` / `service/pre_consume_quota.go` / batch updater — do not call `UpdateUserQuota` directly inside hot relay paths if `BATCH_UPDATE_ENABLED` semantics matter.
- [ ] Subscription resets: `service/subscription_*` paths considered; cron/master-node flag respected.
- [ ] Tiered pricing settlement: amount in quota-units (not raw dollars) at the storage boundary — confirm conversion ratio applied exactly once.

---

## 6. Context keys & request lifecycle

- [ ] New per-request state stored via constants in `constant/context_key.go`. Do not invent stringly-typed keys inline (`c.Set("foo", ...)`).
- [ ] Read with the matching helper / `c.GetString` / `c.GetInt` etc.; type-assert defensively.
- [ ] `RelayInfo` (`relay/common/relay_info.go`) is the per-request carrier across the relay pipeline — prefer adding fields there over piggybacking on `gin.Context` when the value crosses package boundaries.

---

## 7. Concurrency & background tasks

- [ ] Goroutines started in `main.go` are intentional — new background loops should be added there (or behind an `Init*` in `service/`/`controller/`) with a clear lifetime.
- [ ] `common.IsMasterNode` gate for singleton-only tasks (bulk task updaters, scheduled resets).
- [ ] Panics in goroutines have `defer recover()` — main.go shows the pattern (`InitChannelCache` retry).
- [ ] `gopool.Go` (bytedance/gopool) used for bounded goroutines where appropriate — don't spam raw `go func()` for per-request fan-out.
- [ ] Locks: model-level batch updater uses RWMutex over swap-out maps — preserve this pattern, don't reach in and read shared maps directly.
- [ ] Streaming responses: no `gzip.Gzip` middleware on SSE paths (commented out in `main.go` — don't re-enable).

---

## 8. Auth, rate limit, distributor

- [ ] New protected route uses an existing middleware chain (`middleware.UserAuth`, `middleware.AdminAuth`, `middleware.RootAuth`, `middleware.TokenAuth`) — do not roll your own auth check.
- [ ] Rate-limit middleware applied where appropriate (Redis ZSET-based; check `middleware/rate-limit.go`).
- [ ] Distributor (`middleware/distributor.go`) controls channel selection — model-restriction / group / sticky logic lives there. Do not re-implement channel selection in handlers.
- [ ] Sensitive logs: do not log raw API keys, tokens, full request bodies of paid endpoints, or PII. `dto/sensitive.go` lists patterns to mask.

---

## 9. Frontend (default theme)

- [ ] `import` paths use `@/` alias — relative `../../../` chains are a smell.
- [ ] State: TanStack Query for server data, Zustand for UI/client state. Avoid stuffing server response into local component state without a query.
- [ ] Forms: `react-hook-form` + zod resolver. No uncontrolled fetch-then-setState ad-hoc forms.
- [ ] Tailwind: prefer the design tokens in `src/styles/theme.css` (oklch CSS vars) over hex literals. Run `bun run format` (Tailwind class sorting).
- [ ] Components from `src/components/ui/` (shadcn-style) — do not duplicate primitive Button/Input/Dialog wrappers.
- [ ] i18n: every user-visible string wrapped with `t('English source')`. After adding strings, run `bun run i18n:sync`.
- [ ] No raw `fetch` URLs — use the api helper layer and the dev proxy (`/api`, `/mj`, `/pg`).
- [ ] Run before push: `bun run lint && bun run typecheck && bun run format:check && bun run knip`. Knip findings: either remove or whitelist with justification.
- [ ] No new test framework — there is none configured in default theme. Don't import vitest/jest.
- [ ] Copyright header: `bun run copyright:check` passes on new files (or run `bun run copyright`).

For `web/classic/`: same principles but Semi Design components, Vite scripts, React 18, React Router v6 — don't cross-import between themes.

---

## 10. Pull request & commit hygiene (Rule 8)

- [ ] PR description follows `.github/PULL_REQUEST_TEMPLATE.md` (Description, Type, Related Issue, Checklist, Proof of Work). Filled in **human prose**, not boilerplate.
- [ ] Commit message: Conventional Commits (cz config present). Subject ≤ 72 chars. Body explains *why*, not *what*.
- [ ] No `🤖 Generated with Claude Code` line, no `Co-Authored-By: Claude` trailers, no other AI-attribution boilerplate — CI auto-closes PRs containing these strings.
- [ ] One PR = one concern. No drive-by refactors mixed with bug fix.
- [ ] Frontend changes that affect the embedded build (`web/default/dist`) are accompanied by a rebuild note in PR description if `dist/` is checked in.

---

## 11. Protected identifiers (Rule 5)

- [ ] No rename / removal of references to **new-api** (project name) or **QuantumNous** (org/author).
- [ ] License headers, copyright notices, Go module path (`github.com/QuantumNous/new-api`), Docker image refs, README attributions, footer/about pages preserved.
- [ ] Refuse and flag any diff that strips these — even when "cleaning up boilerplate".

---

## 12. Performance & resource hygiene

- [ ] `service.InitHttpClient()` uses the shared HTTP client — do not instantiate `&http.Client{}` per request.
- [ ] Response bodies always closed (`defer resp.Body.Close()`). For streaming relay, the close is delegated to the relay handler.
- [ ] Redis: keys namespaced; no unbounded `KEYS *` scans; use `SCAN` if you must walk.
- [ ] DB queries: `Select` only needed columns on hot paths (channel lookup, token lookup, log search). Avoid `SELECT *` returning JSON blobs you immediately discard.
- [ ] Log volume: high-frequency relay paths should not log per-request at INFO. Use `common.SysLog` sparingly; gate verbose output behind `common.DebugEnabled`.

---

## 13. Testing

- [ ] Tests colocated as `*_test.go`. Run `go test ./...` clean.
- [ ] New cross-DB code: at minimum a SQLite integration test; manual MySQL/PG verification noted in PR.
- [ ] No `t.Skip` on flaky tests without an issue link.
- [ ] Time/random: inject via interface; do not call `time.Now()` / `rand.Int()` directly in pure logic paths under test.

---

## 14. Quick smoke checklist before approving

1. `go vet ./...` clean
2. `go test ./...` clean
3. Frontend: `bun run lint && bun run typecheck` clean in any touched theme
4. Diff respects Rules 1–8 above
5. PR template filled by human, no AI-attribution boilerplate
6. Behavior verified end-to-end (relay request reaches upstream; quota deducted; log row written) — Proof of Work attached
