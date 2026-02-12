# AGENTS.md

This document is optimized for autonomous or AI assistants contributing to **Scrum Poker**, a Go-based real-time estimation tool.

## Repository snapshot

| Component | Location | Notes |
| --- | --- | --- |
| HTTP server + routing | `main.go` | Registers handlers for `/create`, `/room/{id}`, `/join/{id}`, `/vote/{id}`, `/reveal/{id}`, `/reset/{id}`, `/stream/{id}`, `/logout/{id}` and wires templates/static assets. |
| Persistence engine | `store/engine.go` | In-memory room cache mirrored to disk as JSON; configurable via `SCRUMPOKER_STORAGE_DIR` (defaults to `./database`). |
| HTML templates | `./templates/*.html` | Rendered with `html/template` and helper `add` func; ensure templates remain idempotent and XSS-safe. |
| Front-end assets | `./static` | Contains CSS/JS/images plus `/static/robots.txt` and `/static/img/icon/favicon.ico`. |
| Data directory | `./database` | JSON room snapshots written atomically; respect `.gitignore` settings. |
| Infrastructure | `Dockerfile`, `docker-compose.yml`, `gcp/` | Docker image exposes `8080`; compose adds persistence; `gcp/` holds Terraform for cloud deployments. |
| Tooling scripts | `tools/` | Helper scripts; lint shell via `shellcheck`. |

## Quick commands

```bash
# Local run (requires Go >= 1.25)
go run main.go

# Docker
docker build -t scrumpoker . && docker run -d -p 8080:8080 scrumpoker

# Docker Compose (persists ./database)
docker-compose up -d --build

# Format & tidy Go
gofmt -w . && go vet ./... && go mod tidy

# Terraform lint/format (infra changes)
terraform -chdir=gcp fmt && tflint gcp && tfsec gcp

# Shell scripts lint
shellcheck tools/*.sh
```

## Runtime flow reference

1. **Template bootstrap**: `template.ParseGlob("./templates/*.html")` with helper `add` executes before server start.
2. **Store init**: `store.NewStoreEngine(os.Getenv(SCRUMPOKER_STORAGE_DIR))` loads JSON rooms, ensuring a game master and marking dirty state false.
3. **Room lifecycle**:
   - `/create` (POST) → generates UUID room/session, persists new room via `StoreEngine.CreateRoom`, sets cookie `scrumpoker_session_{roomID}`.
   - `/join/{roomID}` (POST) → validates room/name, creates new session via `StoreEngine.JoinRoom`.
   - `/room/{roomID}` (GET) → loads sanitized snapshot via `RoomSnapshot`, enforces cookie membership, renders templates (`room`, `join`, `error`).
   - `/vote/{roomID}` (POST) → `RegisterVote`, resets `Revealed` flag.
   - `/reveal/{roomID}` & `/reset/{roomID}` (POST) → gated by `IsGameMaster`; call `SetReveal`/`ResetVotes`.
   - `/stream/{roomID}` → SSE endpoint streaming JSON state diffs (see `handleStream`).
4. **Graceful shutdown**: `watchForShutdown()` listens for SIGINT/SIGTERM and calls `StoreEngine.FlushAll()`.

## Persistence + data safety

- Room states persisted as `<roomID>.json` via atomic temp-file rename in `store.persistRoomLocked`.
- Ensure every mutating handler acquires the store lock and sets `room.Dirty = true` before returning.
- When introducing new fields in `RoomState` or `PlayerState`, update JSON tags and provide backward-compatible defaults in `bootstrap()`.
- Never embed secrets or PII in room payloads; all user-facing strings originate from form inputs and should be trimmed/validated.

## Front-end considerations

- Templates rely on server-provided flags: `RoomID`, `Room`, `SessionID`, `Player`, `IsGameMaster`.
- SSE listeners in `static/js` expect payloads shaped like ` RoomState` with masked session IDs for non-owners.
- Keep UI responsive; CSS is mobile-first. When adding assets, reference them under `/static/...` and update `http.Handle` routes if necessary.

## Testing & quality gates

| Area | Required actions |
| --- | --- |
| Go code | `gofmt -w .`, `go vet ./...`, optional `go test ./...` (add tests alongside new packages). |
| Terraform | `terraform fmt -recursive gcp`, `tflint gcp`, `tfsec gcp`. |
| Shell scripts | `shellcheck <script>`. |
| Dependency hygiene | Run `go mod tidy` after adding/removing imports; vendor is not used. |

## Common AI task playbooks

- **Add/modify HTTP handler**: update `main.go`, ensure new route registered, implement logic leveraging `store.StoreEngine`, add template/UI hooks, and document expected form fields.
- **Extend room state**: edit `store/engine.go` structs + persistence logic, ensure `bootstrap()` populates defaults, update templates/JS to consume new attributes.
- **Adjust UI flow**: modify templates and static assets; remember to keep forms posting to existing endpoints or update handlers accordingly.
- **Infra updates**: keep Dockerfile minimal (Go build is executed at runtime); when editing `gcp/`, validate with Terraform commands above.
- **Documentation updates**: README for user-facing info, CONTRIBUTING for contributor workflow, and this file for AI-specific context.

## Pull request checklist for agents

1. Explain the problem/solution in the PR description, referencing relevant issues.
2. Include reproduction steps or screenshots for UI changes.
3. Verify formatting/lint commands in "Testing" section of PR template (or describe why not run).
4. Ensure no secrets or test data leak into commits; `database/*.json` should remain local only.
5. Confirm CI/CD artifacts (Docker/Terraform) still build with the documented commands.

Staying within these guardrails keeps automated contributions predictable, reviewable, and production-safe.
