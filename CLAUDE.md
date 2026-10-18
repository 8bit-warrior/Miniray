# CLAUDE.md

Operational guide for AI agents working in this repo. Long-form human docs:
`CONTRIBUTING.md` (setup, testing philosophy) and `frontend/README.md`.
Read those before large changes. This file is the short, must-follow version.
The README and deployment guide describe supported installation and service commands.

## Stack
- Backend: Go 1.27 (`module github.com/8bit-warrior/Miniray`), Gin, GORM.
  Manages Xray-core through an independent systemd service on Linux, or a detached process on Windows; local development also supports a child process (`internal/xray/process.go`) and
  imports `github.com/xtls/xray-core` for config types + gRPC stats/handler/router
  API.
- Storage: SQLite only (`/etc/miniray/miniray.db` on Linux; executable dir on Windows).
  The CGo SQLite driver requires a C compiler (`CGO_ENABLED=1`).
- Frontend: React 19 + Ant Design 6 + Vite 8 + TypeScript in `frontend/`,
  built into `internal/web/dist/` (gitignored) and embedded via `embed.FS`.

## Repo map
- `main.go` — entry point + `miniray` CLI (run, migrate, migrate-db, encrypt-tokens, setting, cert).
- `internal/config/` — env parsing (MINIRAY_DEBUG, MINIRAY_LOG_LEVEL, MINIRAY_LOG_FOLDER,
  MINIRAY_BIN_FOLDER, MINIRAY_SKIP_HSTS, MINIRAY_PORT, MINIRAY_DB_*).
- `internal/database/` + `internal/database/model/` — GORM schema (17 models;
  Inbound, Client, Setting, User are the core), inbound Protocol enum,
  AutoMigrate + hand-written migrations in `db.go`.
- `internal/xray/` — Xray child-process lifecycle, config generation, gRPC API.
- `internal/xray/geodata/` — streaming geosite/geoip `.dat` reader (cached
  category index + paged entries) and `geosite:`/`geoip:`/`ext:` token parsing.
- `internal/tuic/` — TUIC v5 inbounds: `tuic-server` sidecar supervisor, native Go UDP relay traffic metering.
- `internal/amneziawg/` — AmneziaWG protocol shape: instance/peer derivation
  from an inbound, 3.1 obfuscation param generation + validation, port-forward
  spec parsing.
- `internal/amneziawgnet/` — embedded AmneziaWG runtime: amneziawg-go device
  over a gVisor userspace netstack, per-inbound reconcile manager, TCP/UDP
  relay into a loopback per-peer-auth SOCKS5 Xray inbound, port-forward
  listeners, per-peer IPv6 egress aliases.
- `internal/pia/` — PIA WireGuard protocol client (auth, signed server list, `/addKey`).
- `internal/sub/` — subscription server (raw / JSON / Clash).
- `internal/logger/`, `internal/util/` (link, crypto, sys, ldap, …),
  `internal/tunnelmonitor/` — shared infrastructure.
- `internal/web/` — Gin server (embeds `dist/` + `translation/`).
  - `controller/` — panel + REST API handlers; OpenAPI at /panel/api/openapi.json.
  - `service/` — business logic (InboundService, SettingService, XrayService); subpackages outbound/, panel/, integration/.
  - `job/` — local cron jobs (traffic, fail2ban IP-limit, LDAP and resource watchdogs).
  - `middleware/`, `entity/`, `global/`, `session/` (CSRF), `network/`,
    `runtime/` (local proxy mutations), `websocket/`.
  - `locale/` + `translation/` — i18n, 13 embedded locale JSON files.
- `frontend/` — React + TS source (see `frontend/CLAUDE.md`).
- `tools/openapigen/` — Go generator that emits frontend types + Zod/JSON schemas
  into `frontend/src/generated/` from Go structs. `frontend/tools/build-openapi.mjs`
  refreshes schemas in `frontend/public/openapi.json`.

## Hard rules (non-negotiable)
- Correct fix over small fix. Find the root cause and fix it the right way, however
  much code that takes. Size the change by what the correct fix needs, never by
  line count: when the right fix spans many files, or needs a migration, a shared
  helper or a new abstraction, write it. A guard that hides the symptom while the
  cause survives is the wrong fix, however small. Two limits remain:
  - Everything added must be something the correct fix needs. No speculative
    knobs, unused extension points or "while I was here" rewrites. Unrelated
    refactors and cleanups go in their own commit.
  - Stop and ask only when the right fix needs a decision the code cannot answer:
    a deliberate user-visible behaviour change, or two sound designs with a real
    trade-off. Ask with a recommendation. Size alone is never a reason to stop,
    defer or ship a smaller patch.
- Comments in committed Go/TS: 2 lines MAX per comment block. Make the name
  carry the meaning first and rename rather than annotate; spend the 2 lines on
  the *why* a name cannot hold — an invariant, an issue number, a non-obvious
  constraint. Exempt: `//go:build`, `//go:generate`, and other directives.
  HTML `<!-- -->` is fine. (A linter cannot enforce this — you must.)
- New or changed controller routes require matching entries in
  `frontend/public/openapi.json`. Run `make gen` after changing API structs.
- Response examples come from Go `example:` tags via `tools/openapigen`.
  Add API structs to its `StructAllow` allowlist; do not edit generated files.
- A new English i18n key goes in EVERY locale JSON in `internal/web/translation/`
  (13 files) AND must be referenced from `frontend/src` or Go in the SAME commit —
  `frontend/src/test/i18n-dead-keys.test.ts` fails both ways. It is a frontend
  test, so run `npm test`, not just `make test-go`. At runtime the frontend falls
  back to en-US; Go (`internal/web/locale/`) returns "" for an unknown key.
- DB / model changes require a migration in `internal/database/db.go`.
- Every state-changing inbound/client op dispatches through `runtime.Runtime`
  (`internal/web/runtime/`) — never straight to `internal/xray/api.go`, never from
  a controller or cron job. Keep DB mutations and proxy reconciliation consistent across panel and agent.
- Conventional commits: `type(area): short imperative summary`, then a body
  explaining the why. Types in use: `fix`, `feat`, `chore`, `refactor`, `perf`,
  `docs`, `style`.

## Tests: TDD, and only tests that can fail (Go and frontend)
- Work red → green → refactor.
  - Bug: turn the reproduction into a test first, and watch it fail for the
    reported reason.
  - Feature: write the test for the first behaviour before writing its code.
  - Then write the code that makes it pass, and refactor with the suite green.

  If a test was written after the code, prove it anyway: revert the code, watch
  the test go red, then restore. A test that passes either way is worse than no
  test. It certifies nothing, and then gets cited as proof the fix works.
- Every test must name the failure it catches. When no test can reach a change
  (workflow YAML, pure wiring, layout), say so and name the command that
  demonstrates it. Never write a stand-in test.
- Fake tests are forbidden. Delete any you write or meet in the code you touch:
  - tests of a getter, a constant, a rename, a pure map lookup, or an input the
    function can never receive;
  - tests that restate the implementation, such as recomputing the expected
    value with the same formula or asserting that a mock was called exactly the
    way the code calls it;
  - mocking the unit under test, or mocking so much around it that the real
    code path never runs;
  - assertions too weak to fail: `err != nil`, `len > 0`, `toBeDefined()`, or
    `not.toThrow()` alone;
  - golden files or snapshots regenerated to match whatever the code now outputs;
  - extra cases that exercise no distinct branch, and tests written to raise
    coverage.

  One real test that drives the bug through the actual code path beats five
  that restate the code.

## Go conventions
- Stdlib `testing` only (no testify). Table-driven, `t.Run` subtests,
  `t.Helper()` on helpers. Assert the exact value / typed error / emitted
  string, never just `err != nil`. Prefer real deps over mocks: throwaway DB via
  `dbtest.InitDB(t, filepath.Join(t.TempDir(), "miniray.db"))`
  (`internal/database/dbtest`: copies a once-migrated template and registers
  `CloseDB` cleanup; a fresh `database.InitDB` costs ~7x more, ~850ms under
  `-race`); `httptest` for HTTP. Keep `database.InitDB` for reopening a file or
  migrating a hand-built legacy DB. `internal/sub`'s `initSubDB(t)` is the template.
- Code must pass `golangci-lint run` (gofumpt + goimports formatting): `make lint`.
- Xray gRPC e2e and scale tests require `XRAY_E2E_BINARY` or `MINIRAY_SCALE_TEST`;
  an ordinary test run skips those optional checks.

## Frontend conventions (summary; full version in frontend/CLAUDE.md)
- Ant Design 6 only — no Tailwind/shadcn. Targeted tweaks, not rewrites.
- TS strict; `@typescript-eslint/no-explicit-any` is an error. Zod schemas in
  `src/schemas/` are the source of truth; infer types with `z.infer`, never
  hand-write. Do not edit `src/generated/`.
- Node 26 (`.nvmrc`) — `make gen` imports `.ts` directly and needs its type
  stripping; Node 22 dies with `ERR_UNKNOWN_FILE_EXTENSION`. `npm test` includes
  a headless-Chromium Storybook project, so run
  `npx playwright install --with-deps chromium` once or `make verify` fails.
- Editing `frontend/src` does NOT change what users see until the Vite build is
  regenerated into `internal/web/dist/`. In `MINIRAY_DEBUG=true`, HTML is served from
  the frozen embedded FS but JS/CSS off disk — after `npm run build` you MUST
  restart `go run .` or you get a blank page with 404s.
- After touching share-link logic (`src/lib/xray/`), run `npm run test` (golden
  fixtures); regenerate snapshots (`npx vitest run -u`) only for intentional
  output changes, never to make a red test green.

## Build, test, verify
A fresh clone has no `internal/web/dist/`, so a bare `go build ./...` dies with
`pattern all:dist: no matching files found` while ~35 other packages pass — it
reads as a broken repo, not a missing step. Run `make dist-stub` once; every
`make` Go target already depends on it, which is why `make test-go` beats
`go test ./...`. Run `make help` for all targets. The local gate:

    make verify   # gen-check + lint + typecheck + test + build + build-storybook

The release workflow also builds four target archives and exercises the Linux
installer on a disposable native systemd runner before publishing.

Common targets: `make gen` (regenerate Zod/OpenAPI), `make lint` (Go + frontend),
`make test` (Go `-shuffle=on` + frontend), `make race`, `make build`. See `Makefile`.

## Definition of done (before opening a PR)
1. `make verify` passes — its `gen-check` already runs `make gen` and fails on a
   dirty `frontend/src/generated` / `frontend/public/openapi.json`.
2. Diff is focused; refactors are separate from feature work.
