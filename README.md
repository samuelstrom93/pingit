# Pingit

Pingit is a table tennis scorekeeper and tournament web app. This repository contains a Go + SQLite backend and a SvelteKit frontend in one monorepo, built for single-binary deployment with embedded frontend assets.

## Stack

- Go 1.23+
- SQLite via `modernc.org/sqlite`
- Chi router
- Goose migrations
- sqlc config for query generation
- SvelteKit + Tailwind + PWA
- Docker + Caddy for deployment

## Layout

```text
cmd/server           server entrypoint and seed command
internal/api         HTTP routes, middleware, SPA serving
internal/auth        session and magic-link helpers
internal/config      environment loader
internal/db          database bootstrap and models
internal/domain      match, bracket, round-robin, and stats rules
internal/email       email sender interface and dev stub
internal/ws          per-match websocket hub
migrations           goose SQL migrations
web                  SvelteKit frontend
```

## Quickstart

1. Install Go 1.23+ and Node 20 with `corepack` enabled.
2. Install frontend dependencies:

```bash
cd web
pnpm install
```

3. Build the frontend so the Go binary can embed `web/build`:

```bash
cd web
pnpm build
```

4. Start the backend:

```bash
go run ./cmd/server
```

5. Open `http://localhost:8080`.

Default env values:

- `PINGIT_ENV=dev`
- `PINGIT_ADDR=:8080`
- `PINGIT_DB=./pingit-dev.db`
- `PINGIT_BASE_URL=http://localhost:8080`
- `PINGIT_EMAIL_FROM=pingit@example.com`

## Seed Data

Populate the dev database with example users, spaces, matches, and tournaments:

```bash
go run ./cmd/server seed
```

This command wipes the current database contents first.

## Testing

Backend:

```bash
go vet ./...
go test ./...
go build ./...
```

Frontend:

```bash
cd web
pnpm check
pnpm build
```

## Docker

Build the image:

```bash
docker build -t pingit .
```

Run locally with compose:

```bash
docker compose up --build
```

## VPS Deploy

1. Set `PINGIT_DOMAIN` for Caddy.
2. Provide `PINGIT_BASE_URL`, `PINGIT_SUPERADMIN_EMAIL`, and optional Resend credentials.
3. Mount `/data` so the SQLite file persists.
4. Deploy with `docker compose up -d --build`.

## Litestream

Use [`litestream.yml.example`](./litestream.yml.example) as the template for continuous SQLite backups.

Typical activation flow:

1. Copy the example to your server.
2. Fill in bucket credentials.
3. Run Litestream alongside the Pingit container, pointing at `/data/pingit.db`.

## Notes

- Dev emails are written to `./dev-emails.log`.
- The frontend is served by the Go binary using embedded static assets.
