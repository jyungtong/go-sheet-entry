# Deployment Plan — GHCR Public Image, Cross-Build Local, Server Pulls

## Deploy Model

```
[amd64 dev]  cross-compile arm64 binary → build arm64 image → push to GHCR
[arm64 server]  docker compose pull → up -d
```

Registry: `ghcr.io/jyungtong/go-sheet-entry:latest` (public, no server login needed)

---

## Execution Order

- [x] Plan locked
- [x] Step 1: `main.go` — add embed + golang-migrate runner in `initDB`
- [ ] Step 2: `go get github.com/golang-migrate/migrate/v4` + `go mod tidy`
- [ ] Step 3: Verify `go build ./...` (amd64) compiles clean
- [ ] Step 4: Write `Dockerfile` — single-stage, COPY arm64 binary, root distroless-static
- [ ] Step 5: Write `build.sh` — cross-compile arm64 + buildx push
- [ ] Step 6: Write `docker-compose.yml` — pull-only, image:, env_file, volume, restart
- [ ] Step 7: Write `.env` — creds from env.sh, plain KEY=value
- [ ] Step 8: Write `.dockerignore`
- [ ] Step 9: `.gitignore` — add binary; `git rm --cached go-sheet-entry-linux-arm64`

---

## Deferred

- `docker login ghcr.io` PAT on dev — after build success
- Flip GHCR package to public — after first push
- Rotate leaked creds in env.sh / git history — separate task

---

## File Details

### `main.go` changes
- Add imports: `embed`, `errors`, `github.com/golang-migrate/migrate/v4`, `.../database/sqlite`, `.../source/iofs`
- Package-level embed directive:
  ```go
  //go:embed db/migrations/*.sql
  var migrationsFS embed.FS
  ```
- New `runMigrations(db *sql.DB)` function using `iofs.New` + `sqlite.WithInstance` + `m.Up()` tolerating `ErrNoChange`
- `initDB` calls `runMigrations(db)` after `sql.Open`

### `Dockerfile`
```dockerfile
FROM gcr.io/distroless/static-debian12
COPY go-sheet-entry-linux-arm64 /go-sheet-entry
WORKDIR /data
ENTRYPOINT ["/go-sheet-entry"]
```
- Single-stage, COPY-only — no qemu needed
- Root distroless-static — volume writes work, no permission dance
- Migrations baked into binary at cross-compile time

### `build.sh`
```bash
#!/bin/bash
set -e
GOOS=linux GOARCH=arm64 CGO_ENABLED=0 \
  go build -trimpath -ldflags="-s -w" -o go-sheet-entry-linux-arm64 .

docker buildx build --platform linux/arm64 \
  -t ghcr.io/jyungtong/go-sheet-entry:latest \
  --push .
```

### `docker-compose.yml`
```yaml
services:
  bot:
    image: ghcr.io/jyungtong/go-sheet-entry:latest
    env_file: .env
    working_dir: /data
    volumes:
      - dbdata:/data
    restart: unless-stopped
volumes:
  dbdata:
```

### `.env`
```
TELEGRAM_BOT_TOKEN=...
GOOGLE_CLIENT_ID=...
GOOGLE_CLIENT_SECRET=...
GOOGLE_REDIRECT_URL=http://localhost:8080/auth/callback
```
Covered by existing `env.*` gitignore. Lives on server, created once — not in image.

### `.dockerignore`
```
data.db
.git
env.*
```

### Server Deploy Flow
```bash
docker compose pull
docker compose up -d
docker compose logs -f bot
```

---

## Decisions Locked

| # | Decision |
|---|----------|
| Registry | GHCR (`ghcr.io/jyungtong/go-sheet-entry`) |
| Visibility | Public |
| Tag strategy | `:latest` only |
| Binary in git | No (gitignore after step 9) |
| Runtime user | Root distroless-static |
| Migration approach | Option A — golang-migrate as library, embedded in binary |
| OAuth redirect URL | `localhost:8080/auth/callback` — leave as-is |
| `.env` format | Plain `KEY=value` (separate from `env.sh`) |
