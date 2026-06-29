# ban-bot Agent Notes

Single-package Go Discord bot. No subpackages. Entrypoint: `main.go`.

## Required env
- `DISCORD_TOKEN` — bot token (fatal if missing)
- `HONEYPOT_CHANNEL_ID` — channel ID to monitor (fatal if missing)

## Optional env
- `CACHE_TTL_SECONDS` — default 60
- `CACHE_MAX_PER_USER` — default 100

## Common commands
- `make build` → outputs `./ban-bot` (gitignored)
- `make test`  → `go test -v ./...`
- `make run`   → build then execute `./ban-bot`
- `make clean` → remove `./ban-bot`
- Run single test: `go test -run TestName -v ./...`

## Cautions
- `.envrc` contains real secrets; never commit or log it.
- No linter/formatter config present. Use `gofmt` / `go vet` ad-hoc if needed.
- All code lives in package `main`. No internal packages.
- This repo has no CI, no pre-commit hooks, and no README.
