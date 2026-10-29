# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

Assetto Server Manager is a Go web app (chi router, server-rendered Go templates, TypeScript frontend) that manages
an Assetto Corsa dedicated server (`acServer`): race configuration, championships, race weekends, live timing,
results and content upload. This is a fork of JustaPenguin/assetto-server-manager that is being developed further.

## Working Agreement

These rules override the conservative git defaults of the Beads block below.

- **Plan work in beads**: a feature is an epic, its concrete steps are tasks under it (`bd create ... --parent=<epic>`).
  Create/claim the bead before writing code. `todo.md` is the user's scratch list of ideas; turn items into beads
  instead of tracking progress there.
- **Commit after every finished feature/task**: tests green, bead closed, then commit. One logical change per
  commit, imperative English subject in the style of the history (e.g. "Add read-only championship API").
  **Do not push** (git or `bd dolt push`) unless asked.
- User-facing changes go into `CHANGELOG.md` (embedded and shown in the UI); API changes into the API section of
  `README.md`.

## Build & Test

`go.mod` targets Go 1.13 with pinned, old dependencies — don't bump them as a side effect.

The root package imports generated, gitignored embed files (`cmd/server-manager/{static,views}/static_embed.go`,
`internal/changelog/changelog_embed.go`), so run `go generate` on a fresh checkout and after changing
`CHANGELOG.md` or static files, otherwise nothing compiles:

```bash
go install github.com/mjibson/esc@v0.2.0   # once
go generate ./...

go test ./...                               # all tests
go test -run TestNewAPIChampionship .       # single test (root package is `servermanager`)
go vet ./...
```

`make test` additionally copies `fixtures/results/*.json` into `cmd/server-manager/assetto/results` and runs with
`-race`; some results-based tests rely on that. The Makefiles are Unix-only (`rm`, `cp`, `sed`).

Run locally on Windows without a real AC server (fake install in `dev/assetto-seed`), login `admin`/`admin` at
http://localhost:8772:

```powershell
./dev/run.ps1             # generate assets, build, start (frontend built on first run only)
./dev/run.ps1 -Frontend   # also rebuild TypeScript/SCSS
./dev/run.ps1 -Reset      # wipe local DB/config/fake install
./dev/run.ps1 -NoRun      # build only
./dev/run.ps1 -Seed       # fill the DB with test data first (cmd/utils/dev-seed; server must be stopped; idempotent)
docker compose up --build # alternative: production Dockerfile with dev config
```

The server runs from `cmd/server-manager` with `FILESYSTEM_HTML=true`, so template changes only need a browser
refresh; Go changes need a restart. Actually starting a race doesn't work locally (no `acServer` binary).

Frontend: `cd cmd/server-manager/typescript && npm ci && npx gulp build` (`npx gulp` to watch); output goes to
`cmd/server-manager/static`. See `DEVELOPMENT.md` for more (e.g. proxy CA for Docker builds).

## Architecture

- **Root package `servermanager`** holds nearly all logic as flat files grouped by prefix: `race_*` (custom/quick
  races, race weekends, race control/live timing), `championship*`, `content_*` (cars, tracks, weather, uploads),
  `plugin_*` (KissMyRank, Real Penalty, stracker), `store_*`, `*_ini.go` (AC config file generation).
  `cmd/server-manager/main.go` only wires config, store, migrations, UDP and HTTP together.
- **Dependency wiring** is manual in `resolver.go`: `Resolver` lazily builds every manager and handler via
  `resolveXxx()` methods. New managers/handlers must be added there; handlers are also passed into `Router()`.
- **Managers vs. handlers**: `*Manager` types hold business logic (`RaceManager`, `ChampionshipManager`,
  `RaceWeekendManager`, ...); `*Handler` types are thin HTTP layers embedding `BaseHandler` for template rendering.
- **Routing & permissions** (`router.go`): routes are grouped by `AccountHandler` access-level middleware
  (read / write / delete / admin) — put new routes in the group matching the required permission. JSON APIs live
  under `/api/...`.
- **Global config**: `config` (`*Configuration` from `config.yml`, `servermanager_config.go`) is a package-level
  variable; tests set it themselves (`config = &Configuration{}`).
- **Domain model**: `internal/model` holds the persisted domain types (championships, race weekends, custom/quick
  races, `RaceEvent`, accounts, audit entries, live timing data) with their pure methods. It must not depend on the root
  package; anything that needs the install path, server id, build version or track lookups takes them as arguments.
  Stored type names (`RaceEvent` names, gob registrations) keep their old `servermanager.X` names.
- **Persistence**: everything goes through the `Store` interface (`internal/store/store.go`) with two implementations,
  `BoltStore` and `JSONStore` (same package) — new persisted data must be implemented in both. Changes to stored
  structs that need data conversion get a migration appended to `buildMigrations` in `internal/store/migrations.go`
  (never reorder/remove entries; the stored version is the slice index).
- **AC server integration**: `ServerProcess` (`server_process*.go`, OS-specific) starts/stops `acServer` and plugins
  after writing `server_cfg.ini` / `entry_list.ini`. Live data comes back via the AC UDP plugin protocol
  (`pkg/udp`) and is dispatched by `Resolver.UDPCallback` to race control, championship and race weekend managers —
  championship/race weekend progress and results are driven by these events.
- **Forms**: many edit forms are generated from struct tags via `github.com/cj123/formulate` (`formulate.go`).
- **Templates** live in `cmd/server-manager/views` (`layout/`, `pages/`, `partials/`), rendered via `templates.go`
  (sprig + custom funcs); views and static files are embedded into the binary with `esc`.
- `pkg/` holds standalone libraries: `udp` (AC UDP protocol), `acd` (encrypted car data), `when` (timers).

<!-- BEGIN BEADS INTEGRATION v:1 profile:minimal hash:1105d646 -->
## Beads Issue Tracker

This project uses **bd (beads)** for issue tracking. Run `bd prime` to see full workflow context and commands.

### Quick Reference

```bash
bd ready              # Find available work
bd show <id>          # View issue details
bd update <id> --claim  # Claim work
bd close <id>         # Complete work
```

### Rules

- Use `bd` for ALL task tracking — do NOT use TodoWrite, TaskCreate, or markdown TODO lists
- Run `bd prime` for detailed command reference and session close protocol
- Use `bd remember` for persistent knowledge — do NOT use MEMORY.md files

**Architecture in one line:** issues live in a local Dolt DB; sync uses `refs/dolt/data` on your git remote; `.beads/issues.jsonl` is a passive export. See https://github.com/gastownhall/beads/blob/main/docs/core-concepts/sync-concepts.md for details and anti-patterns.

## Agent Context Profiles

The managed Beads block is task-tracking guidance, not permission to override repository, user, or orchestrator instructions.

- **Conservative (default)**: Use `bd` for task tracking. Do not run git commits, git pushes, or Dolt remote sync unless explicitly asked. At handoff, report changed files, validation, and suggested next commands.
- **Minimal**: Keep tool instruction files as pointers to `bd prime`; use the same conservative git policy unless active instructions say otherwise.
- **Team-maintainer**: Only when the repository explicitly opts in, agents may close beads, run quality gates, commit, and push as part of session close. A current "do not commit" or "do not push" instruction still wins.

## Session Completion

This protocol applies when ending a Beads implementation workflow. It is subordinate to explicit user, repository, and orchestrator instructions.

1. **File issues for remaining work** - Create beads for anything that needs follow-up
2. **Run quality gates** (if code changed) - Tests, linters, builds
3. **Update issue status** - Close finished work, update in-progress items
4. **Handle git/sync by active profile**:
   ```bash
   # Conservative/minimal/default: report status and proposed commands; wait for approval.
   git status

   # Team-maintainer opt-in only, unless current instructions forbid it:
   git pull --rebase
   git push
   git status
   ```
5. **Hand off** - Summarize changes, validation, issue status, and any blocked sync/commit/push step

**Critical rules:**
- Explicit user or orchestrator instructions override this Beads block.
- Do not commit or push without clear authority from the active profile or the current user request.
- If a required sync or push is blocked, stop and report the exact command and error.
<!-- END BEADS INTEGRATION -->
