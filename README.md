# Pingit

Pingit is a table tennis scorekeeper and tournament web app. Go + SQLite backend, SvelteKit frontend, single-binary deploy with embedded SPA assets.

## Stack

- **Backend:** Go 1.23, chi router, `modernc.org/sqlite` (pure-Go, CGO-free), goose migrations, `coder/websocket`, ULID IDs
- **Frontend:** SvelteKit (`adapter-static`, SPA), TypeScript strict, Tailwind v3, hand-rolled UI components based on the shadcn-svelte design system, `@vite-pwa/sveltekit` for PWA
- **Auth:** Magic link via email (TTL 15 min, single-use) + session cookie (TTL 30 days)
- **Realtime:** Per-match WebSocket hub broadcasting `score_event`, `game_completed`, `match_completed`, `match_updated`
- **Deploy:** Multi-stage Dockerfile, Caddy reverse proxy with auto-TLS, optional Litestream backups to S3-compatible storage

## Repository layout

```text
cmd/server           server entrypoint and seed command
internal/api         HTTP routes, middleware, SPA serving
internal/auth        session and magic-link helpers
internal/config      environment loader
internal/db          database bootstrap and goose migrations
internal/domain      match state, bracket and round-robin generation, stats
internal/email       email sender interface, console stub, Resend skeleton
internal/ws          per-match websocket hub
migrations           goose SQL migrations (embedded into the binary)
web                  SvelteKit frontend
```

## Local development

Prerequisites: **Go 1.23+** and **Node 20**. Enable `pnpm` via corepack:

```bash
corepack enable
```

Install frontend deps and build the SPA (the Go binary embeds `web/build`):

```bash
cd web
pnpm install
pnpm build
cd ..
```

Run the server:

```bash
go run ./cmd/server
# server listens on :8080
```

Seed demo data (users alice + bob, "Bläckfiskligan" space, players, matches, two tournaments):

```bash
go run ./cmd/server seed
```

The seed wipes the database first; idempotent re-runs are safe.

### Magic-link in dev

Magic-link emails are written both to the structured log (stdout) and appended to `./dev-emails.log`. Open the file, copy the URL, paste it in the browser. Tokens are single-use and expire after 15 minutes.

## Configuration

Environment variables:

| Var | Default | Notes |
|---|---|---|
| `PINGIT_ENV` | `dev` | Set to `prod` to enable secure cookies |
| `PINGIT_ADDR` | `:8080` | TCP listen address |
| `PINGIT_DB` | `./pingit-dev.db` | SQLite file path; created on first start |
| `PINGIT_BASE_URL` | `http://localhost:8080` | Used to construct magic-link URLs |
| `PINGIT_SUPERADMIN_EMAIL` | _(empty)_ | User logging in with this email is granted `is_super_admin` |
| `PINGIT_RESEND_API_KEY` | _(empty)_ | When set, swaps the console stub for the Resend adapter |
| `PINGIT_EMAIL_FROM` | `pingit@example.com` | From-address used by the Resend adapter |

## Tests

```bash
# backend
go vet ./...
go test ./...

# frontend
cd web && pnpm check && pnpm build
```

The backend test suite covers the domain rules (match/game completion at deuce, bracket byes, round-robin pairings, stats) and HTTP integration paths (magic-link round trip, match scoring + completion, post-completion edit + audit log, tournament create/start/list/standings).

## Docker

Build the image (multi-stage: Node → Go → distroless-ish Alpine):

```bash
docker build -t pingit .
```

Compose up with the bundled Caddy:

```bash
PINGIT_DOMAIN=pingit.example.com docker compose up --build
```

The image expects `/data` to be a volume so the SQLite file persists across restarts.

## VPS deploy

1. Provision a small VPS (Hetzner CX22 / DigitalOcean basic / etc).
2. Point an A/AAAA record at the host (e.g. `pingit.your-domain.com`).
3. Copy this repo to the host (`git clone` or push from CI).
4. Set the env in `.env` (or shell): `PINGIT_DOMAIN`, `PINGIT_BASE_URL`, `PINGIT_SUPERADMIN_EMAIL`, optional `PINGIT_RESEND_API_KEY` and `PINGIT_EMAIL_FROM`.
5. `docker compose up -d --build`. Caddy will obtain a TLS cert automatically on first request.

## Litestream backups

[`litestream.yml.example`](./litestream.yml.example) is a template for continuous replication of `pingit.db` to S3-compatible storage (Backblaze B2 by default).

1. Copy the file to `litestream.yml` next to your `docker-compose.yml`.
2. Fill in `bucket`, `endpoint`, and credentials (`B2_KEY_ID`, `B2_APP_KEY`).
3. Run a Litestream sidecar container alongside the Pingit app, mounting the same `/data` volume.

The original SQLite file remains the source of truth; Litestream streams WAL frames continuously and lets you point-in-time restore.

## Architecture notes

- **Single binary, single file:** `web/build` is embedded with `//go:embed all:web/build` (`embed.go`). Migrations are embedded the same way (`migrations/*.sql`) so the runtime never reads from disk for schema upgrades.
- **Players are hybrid:** a player belongs to a space and may be linked to a user (`user_id` set) or anonymous (`user_id` NULL). The `POST /api/players/:id/claim` endpoint promotes an anonymous player to a real user without losing match history.
- **Score granularity:** every tap is persisted as a `score_events` row, enabling poäng-för-poäng replay and richer future analytics.
- **Edits after completion:** `PATCH /api/matches/:id` writes a JSON diff to `match_edits` so corrections are auditable.
- **Realtime is broadcast-only:** the WebSocket hub never accepts writes from the client; all state mutations go through the REST API. Clients subscribe per match and re-fetch on each broadcast.

## Out of scope (today)

- Real email delivery — `internal/email/email.go` still returns from the Resend adapter via the console stub. Plug in the actual Resend API call when you're ready.
- Push notifications.
- Offline-first scoring (clients require connectivity for now).

## Feature parity notes

- Feature flags: the old Azure App Configuration dependency is intentionally not ported. Pingit currently keeps feature scope local and explicit in code/config; if remote toggles become necessary later, add a small provider abstraction instead of coupling the app to Azure-specific APIs.
- `groups_knockout` tournaments: Pingit splits seeded players into groups, creates round-robin group matches, then automatically seeds a knockout bracket from completed group standings. Two groups seed as A1-B2 and B1-A2; a single group seeds the top four overall.
