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

- `DATABASE_URL`: PostgreSQL connection string. Default: `postgres://diskcount:diskcount@localhost:5432/diskcount`.
- `WEB_ADMIN_ADDR`: web admin listen address. Default: `0.0.0.0:47832`.

App settings can be imported from environment variables and then managed from the web admin:

- `TELEGRAM_BOT_TOKEN`: Telegram bot token.
- `REQUEST_TIMEOUT_SECONDS`: HTTP request timeout.
- `USER_AGENT`: scanner HTTP user agent.
- `DISKPRICES_URL`: DiskPrices URL.
- `PRICEPERGIG_ENABLED`, `PRICEPERGIG_API_URL`, `PRICEPERGIG_MARKET`: PricePerGig settings.
- `PRICEPERTB_URLS`: comma-separated PricePerTB URLs.
- `DEALABS_RSS_URLS`, `IDEALO_FEED_URLS`, `IDEALO_PAGE_URLS`, `LEDENICHEUR_FEED_URLS`, `LEDENICHEUR_PAGE_URLS`, `LEBONCOIN_FEED_URLS`: optional configured feeds or pages.
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
- `/users`: authorized Telegram users.

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
- Idealo, leDenicheur, and leboncoin configured feeds/pages.
- Optional Keepa API.
- Optional eBay Browse API.

## Deployment Notes

The included Dockerfile exposes port `47832`. The compose files run PostgreSQL, optional Byparr headless fetch support, and the DiskCount service.

The service runs database migrations at startup using idempotent `CREATE IF NOT EXISTS` and `ALTER TABLE IF NOT EXISTS` statements.
