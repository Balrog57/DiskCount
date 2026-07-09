# DiskCount

DiskCount watches HDD/SSD deals, records price history, and sends Telegram notifications when a user alert matches.

The project is a Go service with two interfaces:

- Telegram: alert creation, alert editing, and deal notifications.
- Web admin: local-network dashboard for supervision, users, configuration, data quality, products, and safe alert administration.

## Quick Start

```powershell
go test ./...
go run ./cmd/diskcount
```

For local containers:

```powershell
docker compose up --build
```

The web admin listens on `0.0.0.0:47832` by default. Protect it with your LAN, firewall, VPN, or reverse proxy; it has no application login in v1.

## Configuration

Bootstrap environment variables:

- `DATABASE_URL`: PostgreSQL connection string. Default: `postgres://<user>:<password>@localhost:5432/diskcount` (set via environment variable).
- `WEB_ADMIN_ADDR`: web admin listen address. Default: `0.0.0.0:47832`.

App settings can be imported from environment variables and then managed from the web admin:

- `TELEGRAM_BOT_TOKEN`: Telegram bot token.
- `REQUEST_TIMEOUT_SECONDS`: HTTP request timeout.
- `USER_AGENT`: scanner HTTP user agent.
- `DISKPRICES_URL`: DiskPrices URL.
- `PRICEPERGIG_ENABLED`, `PRICEPERGIG_API_URL`, `PRICEPERGIG_MARKET`: PricePerGig settings.
- `PRICEPERTB_URLS`: comma-separated PricePerTB URLs.
- `DEALABS_RSS_URLS`, `IDEALO_FEED_URLS`, `LEDENICHEUR_FEED_URLS`, `LEBONCOIN_FEED_URLS`: optional configured RSS feeds.
- `SOURCE_HEADLESS_FALLBACK`, `BYPARR_URL`: optional headless fallback settings.
- `KEEPA_API_KEY`, `KEEPA_ASINS`: optional Keepa settings.
- `EBAY_CLIENT_ID`, `EBAY_CLIENT_SECRET`, `EBAY_SEARCH_QUERIES`: optional eBay settings.
- `NOTIFICATION_PRICE_DROP_PCT`, `TELEGRAM_MESSAGE_DELAY_SECONDS`, `SCRAPE_INTERVAL_CRON`: notification and scanner cadence.

Secret values are masked in the web admin and only replaced when explicitly requested.

## Web Admin

Available pages:

- `/`: overview, service state, sources, counters, latest scanner report.
- `/quality`: data quality per source and reject reasons.
- `/products`: recent best products with source, media, capacity, and price filters.
- `/alerts`: existing alerts with pause, resume, and delete actions only.
- `/config`: persisted app configuration.
- `/metrics/dashboard`: per-source breaker states and last-scan metrics; reset a breaker to force it closed.
- `/users`: authorized Telegram users.

JSON endpoints (no auth — meant for monitors and load balancers):

- `GET /health` / `/healthz` / `/readyz`: returns `200` with `{"status":"ok", ...}` or `503` with `{"status":"degraded", ...}` when the DB is unreachable. Includes the last scan timestamp and per-source breaker states.
- `GET /api/metrics`: stable JSON snapshot of the last scan, breaker states, and per-source metrics.
- `POST /api/sources/breaker/reset`: form-encoded `name=<source>` resets the breaker for that source.

Alert creation and detailed alert editing stay on Telegram. The web admin intentionally does not create alerts.

## Telegram

Useful commands:

- `/start`, `/menu`, `/help`: open the inline navigation.
- `/create`: start the clickable alert creation wizard.
- `/add`: create an alert from text.
- `/alerts`: list and edit your alerts.
- `/pause ID`, `/resume ID`, `/delete ID`: manage an existing alert.
- `/set_max_price ID VALUE`: update the maximum EUR/TB threshold; use `none` to disable it.
- `/set_capacity ID MIN MAX`: update capacity bounds; use `none` for an open bound.
- `/prices`: show current best recorded prices.

Supported text alert keys include `name`, `min_tb`, `max_tb`, `max_eur_tb`, `max_eur_gb`, `condition`, `media`, `category`, `interface`, `discount`, and `cooldown`.

## Data Sources

The scanner is intentionally conservative:

- DiskPrices France public table.
- PricePerGig public API.
- PricePerTB public table.
- Dealabs RSS feeds.
- Idealo, leDenicheur, and leboncoin configured RSS feeds.
- Optional Keepa API.
- Optional eBay Browse API.

### Adding a New Source

1. Create `internal/sources/<name>.go` that implements `sources.Source` (Name + Fetch).
2. Optionally implement the marker interfaces:
   - `Describable.Info()` to populate the admin catalog.
   - `HealthCheckable.HealthCheck(ctx)` so the registry can skip a sick source.
   - `RateLimitable.RateLimit()` to declare a request rate.
3. Register the source via `init()`:
   ```go
   func init() {
       sources.Register(func(r *sources.Registry) sources.Source {
           cfg := r.Config()
           if cfg.MySourceURL == "" { return nil }
           return &MySource{http: r.HTTP(), byparr: r.Byparr(), url: cfg.MySourceURL}
       })
   }
   ```
   Sources are wrapped in a per-source circuit breaker (`sony/gobreaker`)
   and a `RetryingFetcher` that honours `RETRY_MAX_ATTEMPTS`,
   `RETRY_BASE_DELAY_SECONDS`, and `RETRY_MAX_DELAY_SECONDS`. You can use
   `r.HTTP()` for direct fetches, `r.Retry()` for the retry-wrapped
   version, or call `r.Byparr().GetPage(...)` for headless fallback.
4. Add fields to `Config`, declare the env keys in `config.AppSettings`,
   and extend `LoadWithAppValues` to read them.
5. Add `ENABLED_SOURCES=...` to the web admin's config page to opt in
   (an empty list enables every registered source).
6. Write a unit test using `sources.NewTestFetcher` and
   `httptest.NewServer` to exercise the parsing. The `internal/sources/testutil.go`
   helpers cover the common patterns.

## Deployment Notes

The included Dockerfile exposes port `47832`. The compose files run PostgreSQL, optional Byparr headless fetch support, and the DiskCount service.

The service runs database migrations at startup using idempotent `CREATE IF NOT EXISTS` and `ALTER TABLE IF NOT EXISTS` statements.
