package db

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"sort"
	"strings"
	"time"

	"github.com/Balrog57/DiskCount/internal/domain"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type DB struct{ Pool *pgxpool.Pool }

func New(ctx context.Context, databaseURL string) (*DB, error) {
	cfg, err := pgxpool.ParseConfig(databaseURL)
	if err != nil {
		return nil, fmt.Errorf("parse: %w", err)
	}
	p, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		return nil, fmt.Errorf("connect: %w", err)
	}
	if err := p.Ping(ctx); err != nil {
		return nil, fmt.Errorf("ping: %w", err)
	}
	return &DB{Pool: p}, nil
}

func (db *DB) Close() { db.Pool.Close() }

// migrations is the append-only history of schema changes. Each entry runs
// in its own transaction and is recorded in schema_migrations so it is never
// re-applied. To evolve the schema: append a new {N, "..."} entry here. Do
// not edit or reorder existing entries — they must stay idempotent for fresh
// deployments (which run every migration in order) and safe to skip for
// existing ones (which only run the tail).
//
// All statements intentionally use IF NOT EXISTS so the base migration can
// run on a fresh database in one pass without special-casing the "create
// table" vs "add column" distinction.
var migrations = []struct {
	n   int
	sql string
}{
	{n: 1, sql: `
CREATE TABLE IF NOT EXISTS subscribers (chat_id BIGINT PRIMARY KEY, username VARCHAR(255), first_seen_at TIMESTAMPTZ DEFAULT NOW(), last_seen_at TIMESTAMPTZ DEFAULT NOW(), enabled BOOLEAN DEFAULT TRUE);
CREATE TABLE IF NOT EXISTS authorized_users (telegram_user_id BIGINT PRIMARY KEY, label VARCHAR(120) NOT NULL, is_admin BOOLEAN DEFAULT FALSE, enabled BOOLEAN DEFAULT TRUE, created_at TIMESTAMPTZ DEFAULT NOW(), updated_at TIMESTAMPTZ DEFAULT NOW());
CREATE TABLE IF NOT EXISTS alerts (id SERIAL PRIMARY KEY, chat_id BIGINT NOT NULL, owner_user_id BIGINT NOT NULL, name VARCHAR(120) NOT NULL, min_capacity_tb DOUBLE PRECISION, max_capacity_tb DOUBLE PRECISION, capacity_presets JSONB DEFAULT '[]', conditions JSONB DEFAULT '[]', media_types JSONB DEFAULT '[]', drive_categories JSONB DEFAULT '[]', interfaces JSONB DEFAULT '[]', sources JSONB DEFAULT '[]', max_price_per_tb NUMERIC(10,2), min_discount_pct REAL DEFAULT 5.0, cooldown_hours INTEGER DEFAULT 24, enabled BOOLEAN DEFAULT TRUE, created_at TIMESTAMPTZ DEFAULT NOW(), updated_at TIMESTAMPTZ DEFAULT NOW());
CREATE INDEX IF NOT EXISTS idx_alerts_owner ON alerts(owner_user_id);
CREATE TABLE IF NOT EXISTS products (id VARCHAR(80) PRIMARY KEY, source VARCHAR(40) NOT NULL, external_id VARCHAR(255), title TEXT NOT NULL, url TEXT NOT NULL, capacity_tb NUMERIC(10,3) NOT NULL, condition VARCHAR(20), media_type VARCHAR(30), form_factor VARCHAR(120), technology VARCHAR(120), drive_category VARCHAR(40), interfaces JSONB DEFAULT '[]', quality_score INTEGER DEFAULT 0, classification_source VARCHAR(40), canonical_url TEXT, merchant VARCHAR(120), brand VARCHAR(120), model VARCHAR(180), raw_title TEXT, first_seen_at TIMESTAMPTZ DEFAULT NOW(), last_seen_at TIMESTAMPTZ DEFAULT NOW());
CREATE TABLE IF NOT EXISTS price_observations (id SERIAL PRIMARY KEY, product_id VARCHAR(80) REFERENCES products(id), source VARCHAR(40) NOT NULL, observed_at TIMESTAMPTZ DEFAULT NOW(), price_eur NUMERIC(10,2) NOT NULL, price_per_tb NUMERIC(10,2) NOT NULL, quality_score INTEGER DEFAULT 0, raw_json JSONB DEFAULT '{}');
CREATE INDEX IF NOT EXISTS idx_obs_pid ON price_observations(product_id);
CREATE INDEX IF NOT EXISTS idx_obs_ts ON price_observations(observed_at);
CREATE INDEX IF NOT EXISTS idx_obs_latest ON price_observations(product_id, observed_at DESC); -- composite index to avoid costly sort during DISTINCT ON (product_id) in LatestPrices query
CREATE TABLE IF NOT EXISTS notifications (id SERIAL PRIMARY KEY, alert_id INTEGER REFERENCES alerts(id), product_id VARCHAR(80) REFERENCES products(id), sent_at TIMESTAMPTZ DEFAULT NOW(), price_eur NUMERIC(10,2) NOT NULL, price_per_tb NUMERIC(10,2) NOT NULL, discount_pct NUMERIC(6,2), reason VARCHAR(80) NOT NULL, title TEXT NOT NULL, url TEXT NOT NULL);
CREATE INDEX IF NOT EXISTS idx_notif_aid ON notifications(alert_id);
CREATE INDEX IF NOT EXISTS idx_notif_pid ON notifications(product_id);
CREATE INDEX IF NOT EXISTS idx_notif_ts ON notifications(sent_at);
CREATE TABLE IF NOT EXISTS app_config (key TEXT PRIMARY KEY, value TEXT NOT NULL, updated_at TIMESTAMPTZ DEFAULT NOW());
CREATE TABLE IF NOT EXISTS rejected_deals (id SERIAL PRIMARY KEY, source VARCHAR(40) NOT NULL, reason VARCHAR(80) NOT NULL, detail TEXT, title TEXT, url TEXT, observed_at TIMESTAMPTZ DEFAULT NOW(), raw_json JSONB DEFAULT '{}');
CREATE INDEX IF NOT EXISTS idx_rejected_deals_source ON rejected_deals(source);
CREATE INDEX IF NOT EXISTS idx_rejected_deals_observed ON rejected_deals(observed_at);
`},
	// v2: incremental columns added after the initial release. Each ADD
	// COLUMN is idempotent so a fresh DB rolling through v1+v2 ends up at
	// the same schema as a long-running one that applied v2 in production.
	{n: 2, sql: `
ALTER TABLE products ADD COLUMN IF NOT EXISTS quality_score INTEGER DEFAULT 0;
ALTER TABLE products ADD COLUMN IF NOT EXISTS classification_source VARCHAR(40);
ALTER TABLE products ADD COLUMN IF NOT EXISTS canonical_url TEXT;
ALTER TABLE products ADD COLUMN IF NOT EXISTS merchant VARCHAR(120);
ALTER TABLE products ADD COLUMN IF NOT EXISTS brand VARCHAR(120);
ALTER TABLE products ADD COLUMN IF NOT EXISTS model VARCHAR(180);
ALTER TABLE products ADD COLUMN IF NOT EXISTS raw_title TEXT;
ALTER TABLE products ADD COLUMN IF NOT EXISTS recording_method VARCHAR(20);
ALTER TABLE price_observations ADD COLUMN IF NOT EXISTS quality_score INTEGER DEFAULT 0;
ALTER TABLE alerts ADD COLUMN IF NOT EXISTS brands JSONB DEFAULT '[]';
ALTER TABLE alerts ADD COLUMN IF NOT EXISTS keywords JSONB DEFAULT '[]';
ALTER TABLE alerts ADD COLUMN IF NOT EXISTS exclude_keywords JSONB DEFAULT '[]';
ALTER TABLE alerts ADD COLUMN IF NOT EXISTS recording_methods JSONB DEFAULT '[]';
`},
	{n: 3, sql: `
ALTER TABLE alerts ADD COLUMN IF NOT EXISTS discord_enabled BOOLEAN NOT NULL DEFAULT FALSE;
DROP INDEX IF EXISTS idx_alerts_owner;
ALTER TABLE alerts DROP COLUMN IF EXISTS chat_id;
ALTER TABLE alerts DROP COLUMN IF EXISTS owner_user_id;
DROP TABLE IF EXISTS subscribers;
DROP TABLE IF EXISTS authorized_users;
ALTER TABLE notifications DROP CONSTRAINT IF EXISTS notifications_alert_id_fkey;
ALTER TABLE notifications ADD CONSTRAINT notifications_alert_id_fkey FOREIGN KEY (alert_id) REFERENCES alerts(id) ON DELETE CASCADE;
DELETE FROM app_config WHERE key LIKE 'TELEGRAM_%';
`},
	{n: 4, sql: `
ALTER TABLE products ADD COLUMN IF NOT EXISTS canonical_key TEXT;
CREATE INDEX IF NOT EXISTS idx_products_canonical_key ON products(canonical_key) WHERE canonical_key IS NOT NULL;
WITH normalized AS (
	SELECT id,
		CASE WHEN regexp_replace(lower(trim(brand)), '[^[:alnum:]]+', '', 'g') IN ('wd','westerndigital') THEN 'westerndigital' ELSE regexp_replace(lower(trim(brand)), '[^[:alnum:]]+', '', 'g') END AS brand_key,
		regexp_replace(lower(trim(model)), '[^[:alnum:]]+', '', 'g') AS model_key,
		capacity_tb::text AS capacity_key
	FROM products WHERE brand IS NOT NULL AND model IS NOT NULL AND capacity_tb > 0
)
UPDATE products p SET canonical_key=n.brand_key||'|'||n.model_key||'|'||n.capacity_key
FROM normalized n WHERE p.id=n.id AND n.brand_key<>'' AND n.model_key<>'';
`},
	// v5: remove legacy Telegram-era tables from databases that predate the
	// web/Discord alert model. Keep this separate: migrations are append-only.
	{n: 5, sql: `
DROP TABLE IF EXISTS authorized_users;
DROP TABLE IF EXISTS subscribers;
DELETE FROM app_config WHERE key LIKE 'TELEGRAM_%';
`},
	{n: 6, sql: `
ALTER TABLE products ADD COLUMN IF NOT EXISTS availability VARCHAR(20) NOT NULL DEFAULT 'available';
ALTER TABLE products ADD COLUMN IF NOT EXISTS availability_miss_count INTEGER NOT NULL DEFAULT 0;
ALTER TABLE products ADD COLUMN IF NOT EXISTS availability_updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW();
`},
	{n: 7, sql: `
ALTER TABLE products ADD COLUMN IF NOT EXISTS sku VARCHAR(180);
ALTER TABLE products ADD COLUMN IF NOT EXISTS image_url TEXT;
`},
}

// Migrate applies every pending migration in order. Each migration runs in
// its own transaction and is recorded in schema_migrations (created on the
// fly). Already-applied migrations are skipped, so this is safe to call on
// every boot regardless of the database's current state.
//
// Behaviour note: the previous implementation ran the whole history as a
// single Exec every boot, relying on IF NOT EXISTS for idempotency. This
// version produces the same end schema but makes the history reviewable,
// keeps the migration log queryable (SELECT * FROM schema_migrations), and
// lets future changes ship as new numbered entries instead of being
// interleaved into one growing string.
func (db *DB) Migrate(ctx context.Context) error {
	if _, err := db.Pool.Exec(ctx, `
CREATE TABLE IF NOT EXISTS schema_migrations (
	n INTEGER PRIMARY KEY,
	applied_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
)`); err != nil {
		return fmt.Errorf("create schema_migrations: %w", err)
	}
	applied := make(map[int]bool, len(migrations))
	rows, err := db.Pool.Query(ctx, `SELECT n FROM schema_migrations`)
	if err != nil {
		return fmt.Errorf("read schema_migrations: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var n int
		if err := rows.Scan(&n); err != nil {
			return err
		}
		applied[n] = true
	}
	if err := rows.Err(); err != nil {
		return err
	}
	for _, m := range migrations {
		if applied[m.n] {
			continue
		}
		if err := db.applyMigration(ctx, m.n, m.sql); err != nil {
			return fmt.Errorf("migration %d: %w", m.n, err)
		}
	}
	return nil
}

// applyMigration runs one migration's SQL in a transaction and records it.
// The transaction guarantees a migration is either fully applied or fully
// rolled back, so a crash mid-migration never leaves a half-applied state.
func (db *DB) applyMigration(ctx context.Context, n int, sql string) error {
	tx, err := db.Pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if _, err := tx.Exec(ctx, sql); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `INSERT INTO schema_migrations (n) VALUES ($1) ON CONFLICT (n) DO NOTHING`, n); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

type Alert struct {
	ID                                                                            int64
	Name                                                                          string
	MinCapacityTB, MaxCapacityTB, MaxPricePerTB                                   *float64
	CapacityPresets, Conditions, MediaTypes, DriveCategories, Interfaces, Sources []string
	Brands, Keywords, ExcludeKeywords, RecordingMethods                           []string
	MinDiscountPct                                                                float64
	CooldownHours                                                                 int
	Enabled, DiscordEnabled                                                       bool
	CreatedAt, UpdatedAt                                                          time.Time
}
type Product struct {
	ID, Source, Title, URL                                                             string
	ExternalID, Condition, MediaType, FormFactor, Technology, DriveCategory            *string
	ClassificationSource, CanonicalURL, CanonicalKey, Merchant, Brand, Model, RawTitle *string
	RecordingMethod, SKU, ImageURL                                                     *string
	CapacityTB                                                                         float64
	Interfaces                                                                         []string
	QualityScore                                                                       int
	Availability                                                                       domain.Availability
	AvailabilityMissCount                                                              int
	AvailabilityUpdatedAt                                                              time.Time
	FirstSeenAt, LastSeenAt                                                            time.Time
}
type PriceObservation struct {
	ID                   int64
	ProductID, Source    string
	ObservedAt           time.Time
	PriceEUR, PricePerTB float64
	QualityScore         int
	RawJSON              string
}
type Notification struct {
	ID, AlertID                              int64
	ProductID, AlertName, Reason, Title, URL string
	SentAt                                   time.Time
	PriceEUR, PricePerTB                     float64
	DiscountPct                              *float64
}
type CurrentPrice struct {
	ProductID                                                                 string
	Source, Title, URL                                                        string
	Condition, MediaType, DriveCategory, CanonicalKey, Brand, RecordingMethod *string
	SKU, ImageURL, Model                                                      *string
	Interfaces                                                                []string
	CapacityTB                                                                float64
	PriceEUR, PricePerTB                                                      float64
	ObservedAt                                                                time.Time
	Availability                                                              domain.Availability
}

type ProductOffer struct {
	ProductID, Source, Title, URL string
	Condition                     *string
	SKU, ImageURL                 *string
	PriceEUR, PricePerTB          float64
	ObservedAt                    time.Time
	Availability                  domain.Availability
}

type ProductGroup struct {
	CanonicalKey                 string
	Brand, Model                 string
	SKU, ImageURL                *string
	MediaType, DriveCategory     *string
	RecordingMethod              *string
	Interfaces                   []string
	CapacityTB                   float64
	BestPriceEUR, BestPricePerTB float64
	OfferCount                   int
	Availability                 domain.Availability
	ObservedAt                   time.Time
	BestProductID                string
	Offers                       []ProductOffer
}
type Stats struct {
	ActiveAlerts, InactiveAlerts          int64
	Products, Observations, Notifications int64
	RejectedDeals                         int64
	LastObservationAt, LastNotificationAt *time.Time
}

type SourceQuality struct {
	Source                                         string
	Products, Observations, Rejected               int64
	MissingTitle, MissingMedia, MissingCategory    int64
	MissingInterfaces                              int64
	MinPricePerTB, MedianPricePerTB, MaxPricePerTB *float64
}

type RejectReasonStat struct {
	Source, Reason string
	Count          int64
}

type QualityStats struct {
	Sources []SourceQuality
	Reasons []RejectReasonStat
}

func (db *DB) ImportAppConfig(ctx context.Context, values map[string]string) (int, error) {
	if len(values) == 0 {
		return 0, nil
	}
	count := 0
	for key, val := range values {
		if key == "" {
			continue
		}
		tag, err := db.Pool.Exec(ctx, `INSERT INTO app_config (key,value) VALUES ($1,$2) ON CONFLICT(key) DO NOTHING`, key, val)
		if err != nil {
			return count, err
		}
		count += int(tag.RowsAffected())
	}
	return count, nil
}

func (db *DB) ListAppConfig(ctx context.Context) (map[string]string, error) {
	rows, err := db.Pool.Query(ctx, `SELECT key,value FROM app_config ORDER BY key`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make(map[string]string)
	for rows.Next() {
		var key, val string
		if err := rows.Scan(&key, &val); err != nil {
			return nil, err
		}
		out[key] = val
	}
	return out, rows.Err()
}

func (db *DB) SetAppConfig(ctx context.Context, values map[string]string) error {
	tx, err := db.Pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	for key, val := range values {
		if key == "" {
			continue
		}
		_, err := tx.Exec(ctx, `INSERT INTO app_config (key,value) VALUES ($1,$2) ON CONFLICT(key) DO UPDATE SET value=$2, updated_at=NOW()`, key, val)
		if err != nil {
			return err
		}
	}
	return tx.Commit(ctx)
}

// AlertDraft carries all filter slices for alert creation in a single value,
// keeping the CreateAlert signature readable as new filter dimensions are added.
type AlertDraft struct {
	CapacityPresets, Conditions, MediaTypes, DriveCategories, Interfaces, Sources []string
	Brands, Keywords, ExcludeKeywords, RecordingMethods                           []string
	MaxPricePerTB                                                                 *float64
	MinDiscountPct                                                                float64
	CooldownHours                                                                 int
	DiscordEnabled                                                                bool
}

func (db *DB) CreateAlert(ctx context.Context, name string, d AlertDraft) (*Alert, error) {
	a := &Alert{Name: name, MaxPricePerTB: d.MaxPricePerTB, MinDiscountPct: d.MinDiscountPct, CooldownHours: d.CooldownHours, CapacityPresets: d.CapacityPresets, Conditions: d.Conditions, MediaTypes: d.MediaTypes, DriveCategories: d.DriveCategories, Interfaces: d.Interfaces, Sources: d.Sources, Brands: d.Brands, Keywords: d.Keywords, ExcludeKeywords: d.ExcludeKeywords, RecordingMethods: d.RecordingMethods, DiscordEnabled: d.DiscordEnabled, Enabled: true, CreatedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC()}
	const cols = "name,capacity_presets,conditions,media_types,drive_categories,interfaces,sources,brands,keywords,exclude_keywords,recording_methods,max_price_per_tb,min_discount_pct,cooldown_hours,discord_enabled"
	err := db.Pool.QueryRow(ctx, `INSERT INTO alerts (`+cols+`) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15) RETURNING id`,
		name, ja(d.CapacityPresets), ja(d.Conditions), ja(d.MediaTypes), ja(d.DriveCategories), ja(d.Interfaces), ja(d.Sources), ja(d.Brands), ja(d.Keywords), ja(d.ExcludeKeywords), ja(d.RecordingMethods), d.MaxPricePerTB, d.MinDiscountPct, d.CooldownHours, d.DiscordEnabled).Scan(&a.ID)
	return a, err
}

func (db *DB) ListAlerts(ctx context.Context, onlyEnabled bool) ([]Alert, error) {
	q := "SELECT id,name,min_capacity_tb,max_capacity_tb,capacity_presets,conditions,media_types,drive_categories,interfaces,sources,brands,keywords,exclude_keywords,recording_methods,max_price_per_tb,min_discount_pct,cooldown_hours,enabled,discord_enabled,created_at,updated_at FROM alerts"
	if onlyEnabled {
		q += " WHERE enabled=TRUE"
	}
	q += " ORDER BY id"
	return scanAlerts(db.Pool.Query(ctx, q))
}

func (db *DB) SetAlertEnabled(ctx context.Context, aID int64, enabled bool) error {
	_, err := db.Pool.Exec(ctx, "UPDATE alerts SET enabled=$1, updated_at=NOW() WHERE id=$2", enabled, aID)
	return err
}

func (db *DB) DeleteAlert(ctx context.Context, aID int64) error {
	_, err := db.Pool.Exec(ctx, "DELETE FROM alerts WHERE id=$1", aID)
	return err
}

// productUpsertSQL is the shared INSERT...ON CONFLICT statement used by
// UpsertProduct, RecordObservation, and RecordNotification. Centralizing it
// keeps the three call sites in sync as columns are added.
const productUpsertSQL = `INSERT INTO products(id,source,external_id,title,url,capacity_tb,condition,media_type,form_factor,technology,drive_category,interfaces,quality_score,classification_source,canonical_url,canonical_key,merchant,brand,model,raw_title,recording_method,sku,image_url,availability,availability_miss_count,availability_updated_at) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18,$19,$20,$21,$22,$23,'available',0,NOW()) ON CONFLICT(id) DO UPDATE SET title=$4,url=$5,capacity_tb=$6,condition=$7,media_type=$8,form_factor=$9,technology=$10,drive_category=$11,interfaces=$12,quality_score=$13,classification_source=$14,canonical_url=$15,canonical_key=$16,merchant=$17,brand=$18,model=$19,raw_title=$20,recording_method=$21,sku=COALESCE($22,products.sku),image_url=COALESCE($23,products.image_url),availability='available',availability_miss_count=0,availability_updated_at=NOW(),last_seen_at=NOW()`

// productUpsertArgs builds the positional arguments for productUpsertSQL.
// Extracted so the three call sites cannot drift out of sync.
func productUpsertArgs(deal domain.Deal, ifaces []string) []any {
	return []any{
		deal.ProductID(), deal.Source, deal.ExternalID, deal.Title, deal.URL, deal.CapacityTB,
		ptrStr(deal.Condition), ptrStr(deal.MediaType), deal.FormFactor, deal.Technology, ptrStr(deal.DriveCategory),
		ja(ifaces), deal.QualityScore, nilIfEmpty(deal.ClassificationSource), nilIfEmpty(deal.CanonicalURL),
		nilIfEmpty(deal.CanonicalProductKey()), deal.Merchant, deal.Brand, deal.Model, nilIfEmpty(deal.RawTitle), ptrStr(deal.RecordingMethod),
		deal.SKU, deal.ImageURL,
	}
}

func (db *DB) UpsertProduct(ctx context.Context, deal domain.Deal) error {
	ifaces := ifaceStrs(deal.Interfaces)
	_, err := db.Pool.Exec(ctx, productUpsertSQL, productUpsertArgs(deal, ifaces)...)
	return err
}

func (db *DB) RecordObservation(ctx context.Context, deal domain.Deal) error {
	if err := validateObservationDeal(deal); err != nil {
		return err
	}
	tx, _ := db.Pool.Begin(ctx)
	if tx == nil {
		return fmt.Errorf("tx begin failed")
	}
	defer tx.Rollback(ctx)
	ifaces := ifaceStrs(deal.Interfaces)
	_, err := tx.Exec(ctx, productUpsertSQL, productUpsertArgs(deal, ifaces)...)
	if err != nil {
		return err
	}
	raw, _ := json.Marshal(deal.Raw)
	obs := deal.ObservedAt
	if obs.IsZero() {
		obs = time.Now().UTC()
	}
	_, err = tx.Exec(ctx, `INSERT INTO price_observations(product_id,source,observed_at,price_eur,price_per_tb,quality_score,raw_json) VALUES($1,$2,$3,$4,$5,$6,$7)`, deal.ProductID(), deal.Source, obs, deal.PriceEUR, deal.PricePerTB, deal.QualityScore, raw)
	if err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// RecordObservationNoUpsert records a price observation without re-upserting
// the product row. Callers that have already called UpsertProduct for the
// same deal in the same scan should prefer this variant to avoid redundant
// writes on the products table.
//
// ⚡ Bolt optimization: skips one INSERT...ON CONFLICT on the products
// table per observation. In the scanner hot path the product has already
// been upserted, so this drops the per-deal write cost by ~2/3 during a
// notification-heavy scan. Observation insert only touches the
// price_observations table (no transaction needed).
func (db *DB) RecordObservationNoUpsert(ctx context.Context, deal domain.Deal) error {
	if err := validateObservationDeal(deal); err != nil {
		return err
	}
	raw, _ := json.Marshal(deal.Raw)
	obs := deal.ObservedAt
	if obs.IsZero() {
		obs = time.Now().UTC()
	}
	_, err := db.Pool.Exec(ctx, `INSERT INTO price_observations(product_id,source,observed_at,price_eur,price_per_tb,quality_score,raw_json) VALUES($1,$2,$3,$4,$5,$6,$7)`, deal.ProductID(), deal.Source, obs, deal.PriceEUR, deal.PricePerTB, deal.QualityScore, raw)
	return err
}

func (db *DB) RecordRejectedDeal(ctx context.Context, deal domain.Deal, reason, detail string) error {
	raw, _ := json.Marshal(deal.Raw)
	obs := deal.ObservedAt
	if obs.IsZero() {
		obs = time.Now().UTC()
	}
	_, err := db.Pool.Exec(ctx, `INSERT INTO rejected_deals(source,reason,detail,title,url,observed_at,raw_json) VALUES($1,$2,$3,$4,$5,$6,$7)`,
		deal.Source, reason, detail, deal.Title, deal.URL, obs, raw)
	return err
}

func (db *DB) BaselinePricePerTB(ctx context.Context, pid string, before time.Time, days int) (*float64, error) {
	start := before.Add(-time.Duration(days) * 24 * time.Hour)
	// ⚡ Bolt optimization: Compute median directly in PostgreSQL instead of loading all rows into Go memory
	var med *float64
	err := db.Pool.QueryRow(ctx, "SELECT percentile_cont(0.5) WITHIN GROUP (ORDER BY price_per_tb)::float8 FROM price_observations WHERE product_id=$1 AND observed_at>=$2 AND observed_at<$3", pid, start, before).Scan(&med)
	if err != nil {
		return nil, err
	}
	if med == nil {
		return nil, nil
	}
	r := math.Round(*med*100) / 100
	return &r, nil
}

// BaselinePricePerTBMap returns the 30-day median price-per-TB for every
// product ID in the input slice in a single round-trip. Missing products
// and products with no qualifying observations are absent from the map.
//
// ⚡ Bolt optimization: replaces N per-deal calls to BaselinePricePerTB in
// the scanner hot path (each one a percentile_cont over 30 days of rows)
// with one grouped query. On a typical diskprices scan (~200 accepted
// deals) this cuts ~200 sequential median aggregations down to 1.
func (db *DB) BaselinePricePerTBMap(ctx context.Context, productIDs []string, before time.Time, days int) (map[string]float64, error) {
	if len(productIDs) == 0 {
		return map[string]float64{}, nil
	}
	start := before.Add(-time.Duration(days) * 24 * time.Hour)
	rows, err := db.Pool.Query(ctx, `
		SELECT product_id, ROUND(percentile_cont(0.5) WITHIN GROUP (ORDER BY price_per_tb)::numeric, 2)::float8 AS median
		FROM price_observations
		WHERE product_id = ANY($1) AND observed_at >= $2 AND observed_at < $3
		GROUP BY product_id`, productIDs, start, before)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make(map[string]float64, len(productIDs))
	for rows.Next() {
		var pid string
		var med float64
		if err := rows.Scan(&pid, &med); err != nil {
			return nil, err
		}
		out[pid] = med
	}
	return out, rows.Err()
}

func (db *DB) LastNotification(ctx context.Context, aID int64, pid string) (*Notification, error) {
	n := &Notification{}
	err := db.Pool.QueryRow(ctx, "SELECT id,alert_id,product_id,sent_at,price_eur,price_per_tb,discount_pct,reason,title,url FROM notifications WHERE alert_id=$1 AND product_id=$2 ORDER BY sent_at DESC LIMIT 1", aID, pid).Scan(&n.ID, &n.AlertID, &n.ProductID, &n.SentAt, &n.PriceEUR, &n.PricePerTB, &n.DiscountPct, &n.Reason, &n.Title, &n.URL)
	if err == pgx.ErrNoRows {
		return nil, nil
	}
	return n, err
}

func (db *DB) RecentNotifications(ctx context.Context, limit int) ([]Notification, error) {
	if limit <= 0 {
		limit = 20
	}
	rows, err := db.Pool.Query(ctx, `
		SELECT n.id,n.alert_id,n.product_id,a.name,n.sent_at,n.price_eur,n.price_per_tb,n.discount_pct,n.reason,n.title,n.url
		FROM notifications n JOIN alerts a ON a.id=n.alert_id
		ORDER BY n.sent_at DESC LIMIT $1`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]Notification, 0, limit)
	for rows.Next() {
		var n Notification
		if err := rows.Scan(&n.ID, &n.AlertID, &n.ProductID, &n.AlertName, &n.SentAt, &n.PriceEUR, &n.PricePerTB, &n.DiscountPct, &n.Reason, &n.Title, &n.URL); err != nil {
			return nil, err
		}
		out = append(out, n)
	}
	return out, rows.Err()
}

// LastNotificationsMap returns the most recent notification for every
// (alert_id, product_id) pair the scanner might evaluate, in a single
// round-trip. The key is "alertID:productID". Pairs with no notification
// are absent from the map.
//
// This replaces the per-(deal × matching alert) LastNotification call in
// the scanner hot path: a scan with 200 deals × 5 matching alerts used
// to issue up to 1000 sequential indexed lookups; this collapses them
// to one DISTINCT ON query bounded by len(alertIDs) × len(productIDs).
func (db *DB) LastNotificationsMap(ctx context.Context, alertIDs []int64, productIDs []string) (map[string]*Notification, error) {
	if len(alertIDs) == 0 || len(productIDs) == 0 {
		return map[string]*Notification{}, nil
	}
	rows, err := db.Pool.Query(ctx, `
		SELECT DISTINCT ON (alert_id, product_id)
			alert_id, product_id, id, sent_at, price_eur, price_per_tb, discount_pct, reason, title, url
		FROM notifications
		WHERE alert_id = ANY($1) AND product_id = ANY($2)
		ORDER BY alert_id, product_id, sent_at DESC`, alertIDs, productIDs)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make(map[string]*Notification, len(alertIDs)*len(productIDs))
	for rows.Next() {
		n := &Notification{}
		if err := rows.Scan(&n.AlertID, &n.ProductID, &n.ID, &n.SentAt, &n.PriceEUR, &n.PricePerTB, &n.DiscountPct, &n.Reason, &n.Title, &n.URL); err != nil {
			return nil, err
		}
		out[fmt.Sprintf("%d:%s", n.AlertID, n.ProductID)] = n
	}
	return out, rows.Err()
}

func (db *DB) RecordNotification(ctx context.Context, alert *Alert, deal domain.Deal, reason string, disc *float64) error {
	if err := validateObservationDeal(deal); err != nil {
		return err
	}
	tx, _ := db.Pool.Begin(ctx)
	if tx == nil {
		return fmt.Errorf("tx begin")
	}
	defer tx.Rollback(ctx)
	ifaces := ifaceStrs(deal.Interfaces)
	_, err := tx.Exec(ctx, productUpsertSQL, productUpsertArgs(deal, ifaces)...)
	if err != nil {
		return err
	}
	_, err = tx.Exec(ctx, `INSERT INTO notifications(alert_id,product_id,price_eur,price_per_tb,discount_pct,reason,title,url) VALUES($1,$2,$3,$4,$5,$6,$7,$8)`, alert.ID, deal.ProductID(), deal.PriceEUR, deal.PricePerTB, disc, reason, deal.Title, deal.URL)
	if err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// RecordNotificationNoUpsert records a notification without re-upserting the
// product row. The scanner already called UpsertProduct earlier in the loop,
// so this variant avoids a redundant write on the products table per notified
// deal.
//
// ⚡ Bolt optimization: the notification insert only touches the notifications
// table (no transaction needed), saving one BEGIN/COMMIT round-trip plus one
// product upsert per notification.
func (db *DB) RecordNotificationNoUpsert(ctx context.Context, alert *Alert, deal domain.Deal, reason string, disc *float64) error {
	if err := validateObservationDeal(deal); err != nil {
		return err
	}
	_, err := db.Pool.Exec(ctx, `INSERT INTO notifications(alert_id,product_id,price_eur,price_per_tb,discount_pct,reason,title,url) VALUES($1,$2,$3,$4,$5,$6,$7,$8)`, alert.ID, deal.ProductID(), deal.PriceEUR, deal.PricePerTB, disc, reason, deal.Title, deal.URL)
	return err
}

func (db *DB) LatestPrices(ctx context.Context, limit int) ([]CurrentPrice, error) {
	if limit <= 0 {
		limit = 10
	}
	rows, err := db.Pool.Query(ctx, `
WITH latest AS (
	SELECT DISTINCT ON (product_id)
		product_id, source, observed_at, price_eur, price_per_tb
	FROM price_observations
	WHERE price_per_tb > 0 AND quality_score >= 70
	ORDER BY product_id, observed_at DESC
)
SELECT l.product_id, l.source, p.title, p.url, p.condition, p.media_type, p.drive_category, p.canonical_key, p.brand, p.recording_method, p.sku, p.image_url, p.model, p.interfaces, p.capacity_tb, l.price_eur, l.price_per_tb, l.observed_at, p.availability
FROM latest l
JOIN products p ON p.id = l.product_id
WHERE p.quality_score >= 50
ORDER BY (p.availability='available') DESC, l.price_per_tb ASC, l.observed_at DESC
LIMIT $1`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []CurrentPrice
	for rows.Next() {
		var p CurrentPrice
		if err := rows.Scan(&p.ProductID, &p.Source, &p.Title, &p.URL, &p.Condition, &p.MediaType, &p.DriveCategory, &p.CanonicalKey, &p.Brand, &p.RecordingMethod, &p.SKU, &p.ImageURL, &p.Model, jsonScan(&p.Interfaces), &p.CapacityTB, &p.PriceEUR, &p.PricePerTB, &p.ObservedAt, &p.Availability); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

// ListProductGroups returns only groups with a complete canonical identity.
// Products without brand+model+capacity are deliberately excluded; titles are
// never used as a grouping fallback.
func (db *DB) ListProductGroups(ctx context.Context, limit int) ([]ProductGroup, error) {
	if limit <= 0 {
		limit = 50
	}
	rows, err := db.Pool.Query(ctx, `
WITH latest AS (
 SELECT DISTINCT ON (o.product_id) o.product_id,o.source,o.price_eur,o.price_per_tb,o.observed_at
 FROM price_observations o JOIN products p ON p.id=o.product_id
 WHERE p.canonical_key IS NOT NULL AND o.price_per_tb > 0 AND o.quality_score >= 70
 ORDER BY o.product_id,o.observed_at DESC
), group_keys AS (
 SELECT p.canonical_key,MIN(l.price_per_tb) AS best_price
 FROM latest l JOIN products p ON p.id=l.product_id
 GROUP BY p.canonical_key ORDER BY best_price LIMIT $1
)
SELECT p.canonical_key,p.brand,p.model,p.sku,p.image_url,p.media_type,p.drive_category,p.recording_method,p.interfaces,p.capacity_tb,l.product_id,l.source,p.title,p.url,p.condition,l.price_eur,l.price_per_tb,l.observed_at,p.availability
FROM latest l JOIN products p ON p.id=l.product_id
JOIN group_keys g ON g.canonical_key=p.canonical_key
ORDER BY p.canonical_key,l.price_per_tb ASC`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	groups := make([]ProductGroup, 0)
	byKey := make(map[string]int)
	for rows.Next() {
		var key string
		var brand, model string
		var capacity float64
		var offer ProductOffer
		var sku, imageURL, media, category, recording *string
		var ifaces []string
		if err := rows.Scan(&key, &brand, &model, &sku, &imageURL, &media, &category, &recording, jsonScan(&ifaces), &capacity, &offer.ProductID, &offer.Source, &offer.Title, &offer.URL, &offer.Condition, &offer.PriceEUR, &offer.PricePerTB, &offer.ObservedAt, &offer.Availability); err != nil {
			return nil, err
		}
		offer.SKU, offer.ImageURL = sku, imageURL
		i, ok := byKey[key]
		if !ok {
			i = len(groups)
			byKey[key] = i
			groups = append(groups, ProductGroup{
				CanonicalKey: key, Brand: brand, Model: model, SKU: sku, ImageURL: imageURL,
				MediaType: media, DriveCategory: category, RecordingMethod: recording, Interfaces: ifaces,
				CapacityTB: capacity, BestPriceEUR: offer.PriceEUR, BestPricePerTB: offer.PricePerTB,
				Availability: offer.Availability, ObservedAt: offer.ObservedAt, BestProductID: offer.ProductID,
			})
		}
		groups[i].Offers = append(groups[i].Offers, offer)
		groups[i].OfferCount = len(groups[i].Offers)
		if groups[i].ImageURL == nil && imageURL != nil {
			groups[i].ImageURL = imageURL
		}
		if groups[i].SKU == nil && sku != nil {
			groups[i].SKU = sku
		}
	}
	return groups, rows.Err()
}

func (db *DB) ProductOffers(ctx context.Context, canonicalKey string) ([]ProductOffer, error) {
	if canonicalKey == "" {
		return nil, nil
	}
	rows, err := db.Pool.Query(ctx, `
WITH latest AS (
	SELECT DISTINCT ON (o.product_id) o.product_id,o.source,o.price_eur,o.price_per_tb,o.observed_at
	FROM price_observations o JOIN products p ON p.id=o.product_id
	WHERE p.canonical_key=$1 AND o.price_per_tb > 0 AND o.quality_score >= 70
	ORDER BY o.product_id,o.observed_at DESC
)
SELECT l.product_id,l.source,p.title,p.url,p.condition,p.sku,p.image_url,l.price_eur,l.price_per_tb,l.observed_at,p.availability
FROM latest l JOIN products p ON p.id=l.product_id
ORDER BY (p.availability='available') DESC,l.price_per_tb,l.observed_at DESC`, canonicalKey)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var offers []ProductOffer
	for rows.Next() {
		var offer ProductOffer
		if err := rows.Scan(&offer.ProductID, &offer.Source, &offer.Title, &offer.URL, &offer.Condition, &offer.SKU, &offer.ImageURL, &offer.PriceEUR, &offer.PricePerTB, &offer.ObservedAt, &offer.Availability); err != nil {
			return nil, err
		}
		offers = append(offers, offer)
	}
	return offers, rows.Err()
}

// GetProduct fetches a single product by its ID for the detail page.
func (db *DB) GetProduct(ctx context.Context, productID string) (*Product, error) {
	p := &Product{}
	err := db.Pool.QueryRow(ctx, `SELECT id,source,external_id,title,url,capacity_tb,condition,media_type,form_factor,technology,drive_category,interfaces,quality_score,classification_source,canonical_url,canonical_key,merchant,brand,model,raw_title,recording_method,sku,image_url,availability,availability_miss_count,availability_updated_at,first_seen_at,last_seen_at FROM products WHERE id=$1`, productID).Scan(
		&p.ID, &p.Source, &p.ExternalID, &p.Title, &p.URL, &p.CapacityTB, &p.Condition, &p.MediaType, &p.FormFactor, &p.Technology, &p.DriveCategory, jsonScan(&p.Interfaces), &p.QualityScore, &p.ClassificationSource, &p.CanonicalURL, &p.CanonicalKey, &p.Merchant, &p.Brand, &p.Model, &p.RawTitle, &p.RecordingMethod, &p.SKU, &p.ImageURL, &p.Availability, &p.AvailabilityMissCount, &p.AvailabilityUpdatedAt, &p.FirstSeenAt, &p.LastSeenAt)
	if err == pgx.ErrNoRows {
		return nil, nil
	}
	return p, err
}

// GetProductByCanonicalKey returns a representative product for a family page.
func (db *DB) GetProductByCanonicalKey(ctx context.Context, key string) (*Product, error) {
	key = strings.TrimSpace(key)
	if key == "" {
		return nil, nil
	}
	var id string
	err := db.Pool.QueryRow(ctx, `SELECT id FROM products WHERE canonical_key=$1 ORDER BY last_seen_at DESC LIMIT 1`, key).Scan(&id)
	if err == pgx.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return db.GetProduct(ctx, id)
}

type CatalogQuery struct {
	Search, Source, Media, Condition, Availability, Brand, Category, Interface, Recording, Sort string
	MinTB, MaxTB, MaxEURTB                                                                      *float64
	Limit, Offset                                                                               int
}

func catalogWhere(q CatalogQuery, grouped bool) (string, []any) {
	var b strings.Builder
	args := make([]any, 0, 12)
	add := func(clause string, v any) {
		args = append(args, v)
		fmt.Fprintf(&b, " AND "+clause, len(args))
	}
	if grouped {
		b.WriteString(" AND p.canonical_key IS NOT NULL")
	} else {
		b.WriteString(" AND p.canonical_key IS NULL")
	}
	if q.Source != "" {
		add("l.source=$%d", q.Source)
	}
	if q.Media != "" {
		add("p.media_type=$%d", q.Media)
	}
	if q.Condition != "" {
		add("p.condition=$%d", q.Condition)
	}
	if q.Availability != "" {
		add("p.availability=$%d", q.Availability)
	}
	if q.Brand != "" {
		add(`(
  CASE WHEN regexp_replace(lower(trim(COALESCE(p.brand,''))), '[^[:alnum:]]+', '', 'g') IN ('wd','westerndigital')
  THEN 'westerndigital'
  ELSE regexp_replace(lower(trim(COALESCE(p.brand,''))), '[^[:alnum:]]+', '', 'g')
  END
)=$%d`, brandFacetKey(q.Brand))
	}
	if q.Category != "" {
		add("p.drive_category=$%d", q.Category)
	}
	if q.Recording != "" {
		add("p.recording_method=$%d", q.Recording)
	}
	if q.Interface != "" {
		add("p.interfaces ? $%d", q.Interface)
	}
	if q.MinTB != nil {
		add("p.capacity_tb>=$%d", *q.MinTB)
	}
	if q.MaxTB != nil {
		add("p.capacity_tb<=$%d", *q.MaxTB)
	}
	if q.MaxEURTB != nil {
		add("l.price_per_tb<=$%d", *q.MaxEURTB)
	}
	return b.String(), args
}

func catalogOrder(sort string) string {
	switch sort {
	case "price":
		return "g.best_price_eur ASC, g.best_price ASC"
	case "freshness":
		return "g.observed_at DESC, g.best_price ASC"
	case "sellers":
		return "g.offer_count DESC, g.best_price ASC"
	default:
		return "g.best_price ASC, g.observed_at DESC"
	}
}

// CatalogGroups returns one family card per canonical_key, with SQL filters
// and pagination. Total is the unpaginated match count.
func (db *DB) CatalogGroups(ctx context.Context, q CatalogQuery) ([]ProductGroup, int, error) {
	if q.Limit <= 0 || q.Limit > 200 {
		q.Limit = 48
	}
	if q.Offset < 0 {
		q.Offset = 0
	}
	where, args := catalogWhere(q, true)
	if s := strings.TrimSpace(q.Search); s != "" {
		args = append(args, "%"+s+"%")
		where += fmt.Sprintf(" AND (p.title ILIKE $%d OR COALESCE(p.brand,'') ILIKE $%d OR COALESCE(p.model,'') ILIKE $%d OR COALESCE(p.sku,'') ILIKE $%d)", len(args), len(args), len(args), len(args))
	}
	countSQL := `
WITH latest AS (
 SELECT DISTINCT ON (o.product_id) o.product_id,o.source,o.price_eur,o.price_per_tb,o.observed_at
 FROM price_observations o JOIN products p ON p.id=o.product_id
 WHERE o.price_per_tb > 0 AND o.quality_score >= 70
 ORDER BY o.product_id,o.observed_at DESC
)
SELECT COUNT(DISTINCT p.canonical_key)
FROM latest l JOIN products p ON p.id=l.product_id
WHERE p.quality_score >= 50` + where
	var total int
	if err := db.Pool.QueryRow(ctx, countSQL, args...).Scan(&total); err != nil {
		return nil, 0, err
	}
	args = append(args, q.Limit, q.Offset)
	limitPH, offsetPH := len(args)-1, len(args)
	listSQL := fmt.Sprintf(`
WITH latest AS (
 SELECT DISTINCT ON (o.product_id) o.product_id,o.source,o.price_eur,o.price_per_tb,o.observed_at
 FROM price_observations o JOIN products p ON p.id=o.product_id
 WHERE o.price_per_tb > 0 AND o.quality_score >= 70
 ORDER BY o.product_id,o.observed_at DESC
), grouped AS (
 SELECT p.canonical_key,
  MIN(l.price_per_tb) AS best_price,
  (ARRAY_AGG(l.price_eur ORDER BY l.price_per_tb, l.observed_at DESC))[1] AS best_price_eur,
  COUNT(*) AS offer_count,
  MAX(l.observed_at) AS observed_at,
  (ARRAY_AGG(p.brand ORDER BY l.price_per_tb))[1] AS brand,
  (ARRAY_AGG(p.model ORDER BY l.price_per_tb))[1] AS model,
  (ARRAY_AGG(p.sku ORDER BY l.price_per_tb) FILTER (WHERE p.sku IS NOT NULL))[1] AS sku,
  (ARRAY_AGG(p.image_url ORDER BY l.price_per_tb) FILTER (WHERE p.image_url IS NOT NULL))[1] AS image_url,
  (ARRAY_AGG(p.media_type ORDER BY l.price_per_tb))[1] AS media_type,
  (ARRAY_AGG(p.drive_category ORDER BY l.price_per_tb))[1] AS drive_category,
  (ARRAY_AGG(p.recording_method ORDER BY l.price_per_tb))[1] AS recording_method,
  (ARRAY_AGG(p.interfaces ORDER BY l.price_per_tb))[1] AS interfaces,
  (ARRAY_AGG(p.capacity_tb ORDER BY l.price_per_tb))[1] AS capacity_tb,
  (ARRAY_AGG(p.availability ORDER BY (p.availability='available') DESC, l.price_per_tb))[1] AS availability,
  (ARRAY_AGG(l.product_id ORDER BY l.price_per_tb))[1] AS best_product_id
 FROM latest l JOIN products p ON p.id=l.product_id
 WHERE p.quality_score >= 50%s
 GROUP BY p.canonical_key
)
SELECT canonical_key,brand,model,sku,image_url,media_type,drive_category,recording_method,interfaces,capacity_tb,best_price_eur,best_price,offer_count,availability,observed_at,best_product_id
FROM grouped g
ORDER BY %s
LIMIT $%d OFFSET $%d`, where, catalogOrder(q.Sort), limitPH, offsetPH)
	rows, err := db.Pool.Query(ctx, listSQL, args...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	var out []ProductGroup
	for rows.Next() {
		var g ProductGroup
		var offerCount int64
		if err := rows.Scan(&g.CanonicalKey, &g.Brand, &g.Model, &g.SKU, &g.ImageURL, &g.MediaType, &g.DriveCategory, &g.RecordingMethod, jsonScan(&g.Interfaces), &g.CapacityTB, &g.BestPriceEUR, &g.BestPricePerTB, &offerCount, &g.Availability, &g.ObservedAt, &g.BestProductID); err != nil {
			return nil, 0, err
		}
		g.OfferCount = int(offerCount)
		out = append(out, g)
	}
	return out, total, rows.Err()
}

func (db *DB) UngroupedPrices(ctx context.Context, q CatalogQuery) ([]CurrentPrice, error) {
	if q.Limit <= 0 || q.Limit > 200 {
		q.Limit = 48
	}
	where, args := catalogWhere(q, false)
	if s := strings.TrimSpace(q.Search); s != "" {
		args = append(args, "%"+s+"%")
		where += fmt.Sprintf(" AND (p.title ILIKE $%d OR COALESCE(p.brand,'') ILIKE $%d OR COALESCE(p.model,'') ILIKE $%d OR COALESCE(p.sku,'') ILIKE $%d)", len(args), len(args), len(args), len(args))
	}
	args = append(args, q.Limit)
	sql := fmt.Sprintf(`
WITH latest AS (
 SELECT DISTINCT ON (o.product_id) o.product_id,o.source,o.price_eur,o.price_per_tb,o.observed_at
 FROM price_observations o JOIN products p ON p.id=o.product_id
 WHERE o.price_per_tb > 0 AND o.quality_score >= 70
 ORDER BY o.product_id,o.observed_at DESC
)
SELECT l.product_id,l.source,p.title,p.url,p.condition,p.media_type,p.drive_category,p.canonical_key,p.brand,p.recording_method,p.sku,p.image_url,p.model,p.interfaces,p.capacity_tb,l.price_eur,l.price_per_tb,l.observed_at,p.availability
FROM latest l JOIN products p ON p.id=l.product_id
WHERE p.quality_score >= 50%s
ORDER BY (p.availability='available') DESC, l.price_per_tb ASC
LIMIT $%d`, where, len(args))
	rows, err := db.Pool.Query(ctx, sql, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []CurrentPrice
	for rows.Next() {
		var p CurrentPrice
		if err := rows.Scan(&p.ProductID, &p.Source, &p.Title, &p.URL, &p.Condition, &p.MediaType, &p.DriveCategory, &p.CanonicalKey, &p.Brand, &p.RecordingMethod, &p.SKU, &p.ImageURL, &p.Model, jsonScan(&p.Interfaces), &p.CapacityTB, &p.PriceEUR, &p.PricePerTB, &p.ObservedAt, &p.Availability); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

func (db *DB) CatalogFacets(ctx context.Context) (brands, categories, interfaces, recordings, sources []string, err error) {
	rows, err := db.Pool.Query(ctx, `
SELECT DISTINCT p.brand, p.drive_category, p.recording_method, p.source, p.interfaces
FROM products p
WHERE p.quality_score >= 50 AND p.canonical_key IS NOT NULL`)
	if err != nil {
		return nil, nil, nil, nil, nil, err
	}
	defer rows.Close()
	bset, cset, iset, rset, sset := map[string]bool{}, map[string]bool{}, map[string]bool{}, map[string]bool{}, map[string]bool{}
	for rows.Next() {
		var brand, cat, rec, src *string
		var ifaces []string
		if err := rows.Scan(&brand, &cat, &rec, &src, jsonScan(&ifaces)); err != nil {
			return nil, nil, nil, nil, nil, err
		}
		if brand != nil && *brand != "" {
			bset[canonicalBrandFacet(*brand)] = true
		}
		if cat != nil && *cat != "" {
			cset[*cat] = true
		}
		if rec != nil && *rec != "" {
			rset[*rec] = true
		}
		if src != nil && *src != "" {
			sset[*src] = true
		}
		for _, iface := range ifaces {
			if iface != "" {
				iset[iface] = true
			}
		}
	}
	return sortedKeys(bset), sortedKeys(cset), sortedKeys(iset), sortedKeys(rset), sortedKeys(sset), rows.Err()
}

func sortedKeys(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// brandFacetKey normalises a brand filter value the same way canonical_key
// collapses WD / Western Digital so the catalog facet and SQL filter agree.
func brandFacetKey(brand string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(strings.TrimSpace(brand)) {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
		}
	}
	key := b.String()
	if key == "wd" || key == "westerndigital" {
		return "westerndigital"
	}
	return key
}

// canonicalBrandFacet returns the display brand used in filter dropdowns.
// WD and Western Digital collapse to a single "Western Digital" option.
func canonicalBrandFacet(brand string) string {
	if brandFacetKey(brand) == "westerndigital" {
		return "Western Digital"
	}
	return brand
}

// MarkSourceMissing advances absence only after a successful source scan.
func (db *DB) MarkSourceMissing(ctx context.Context, source string, seen []string, threshold int) error {
	if threshold < 1 {
		threshold = 3
	}
	_, err := db.Pool.Exec(ctx, `UPDATE products SET availability_miss_count=availability_miss_count+1, availability=CASE WHEN availability_miss_count+1 >= $3 THEN 'unavailable' ELSE availability END, availability_updated_at=NOW() WHERE source=$1 AND (COALESCE(cardinality($2::text[]),0)=0 OR NOT id=ANY($2))`, source, seen, threshold)
	return err
}

// LastSeenMap returns the most recent observation timestamp for every
// product ID in the input slice. Missing products are absent from the
// map. The function is a single round-trip so callers that need to
// detect "back in stock" across many products stay fast.
func (db *DB) LastSeenMap(ctx context.Context, productIDs []string) (map[string]time.Time, error) {
	if len(productIDs) == 0 {
		return map[string]time.Time{}, nil
	}
	rows, err := db.Pool.Query(ctx, `SELECT id, last_seen_at FROM products WHERE id = ANY($1)`, productIDs)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make(map[string]time.Time, len(productIDs))
	for rows.Next() {
		var id string
		var t time.Time
		if err := rows.Scan(&id, &t); err != nil {
			return nil, err
		}
		out[id] = t
	}
	return out, rows.Err()
}

// PriceHistoryPoint is a single price observation for charting.
type PriceHistoryPoint struct {
	ObservedAt           time.Time
	PriceEUR, PricePerTB float64
	Source               string
}

// PriceHistory returns price observations for a product within the given
// number of days, ordered chronologically for charting.
func (db *DB) PriceHistory(ctx context.Context, productID string, days int) ([]PriceHistoryPoint, error) {
	if days <= 0 {
		days = 30
	}
	start := time.Now().UTC().Add(-time.Duration(days) * 24 * time.Hour)
	rows, err := db.Pool.Query(ctx, `SELECT observed_at, price_eur, price_per_tb, source FROM price_observations WHERE product_id=$1 AND observed_at >= $2 ORDER BY observed_at ASC`, productID, start)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []PriceHistoryPoint
	for rows.Next() {
		var pt PriceHistoryPoint
		if err := rows.Scan(&pt.ObservedAt, &pt.PriceEUR, &pt.PricePerTB, &pt.Source); err != nil {
			return nil, err
		}
		out = append(out, pt)
	}
	return out, rows.Err()
}

func (db *DB) PriceHistoryByKey(ctx context.Context, canonicalKey string, days int) ([]PriceHistoryPoint, error) {
	if days <= 0 {
		days = 30
	}
	start := time.Now().UTC().Add(-time.Duration(days) * 24 * time.Hour)
	rows, err := db.Pool.Query(ctx, `
SELECT o.observed_at, o.price_eur, o.price_per_tb, o.source
FROM price_observations o JOIN products p ON p.id=o.product_id
WHERE p.canonical_key=$1 AND o.observed_at >= $2
ORDER BY o.observed_at ASC`, canonicalKey, start)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []PriceHistoryPoint
	for rows.Next() {
		var pt PriceHistoryPoint
		if err := rows.Scan(&pt.ObservedAt, &pt.PriceEUR, &pt.PricePerTB, &pt.Source); err != nil {
			return nil, err
		}
		out = append(out, pt)
	}
	return out, rows.Err()
}

// SparklinePoint is a single point on a product's 7-day sparkline. The
// goal is to be cheap enough to render for every row on the products
// page: one query, one row per product, at most 50 observations.
type SparklinePoint struct {
	ObservedAt time.Time
	PricePerTB float64
}

// Sparklines returns a small 7-day history per product, capped at
// `maxPoints` observations each. The result is keyed by product ID so
// the rendering layer can do a single map lookup per row. Empty
// products are absent from the map.
//
// The previous implementation used DISTINCT ON (product_id), which
// collapses every product to a single row — so each sparkline had at
// most ONE point and computeSparklinePoints (which needs >=2) never
// drew anything. The ROW_NUMBER() window keeps up to `maxPoints` rows
// per product, which is what the feature actually needs.
func (db *DB) Sparklines(ctx context.Context, productIDs []string, days int, maxPoints int) (map[string][]SparklinePoint, error) {
	if len(productIDs) == 0 {
		return map[string][]SparklinePoint{}, nil
	}
	if days <= 0 {
		days = 7
	}
	if maxPoints <= 0 || maxPoints > 50 {
		maxPoints = 30
	}
	start := time.Now().UTC().Add(-time.Duration(days) * 24 * time.Hour)
	rows, err := db.Pool.Query(ctx, `
		SELECT product_id, observed_at, price_per_tb
		FROM (
			SELECT product_id, observed_at, price_per_tb,
			       ROW_NUMBER() OVER (PARTITION BY product_id ORDER BY observed_at DESC) AS rn
			FROM price_observations
			WHERE product_id = ANY($1) AND observed_at >= $2
		) recent
		WHERE rn <= $3
		ORDER BY product_id, observed_at ASC
	`, productIDs, start, maxPoints)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make(map[string][]SparklinePoint, len(productIDs))
	for rows.Next() {
		var pid string
		var pt SparklinePoint
		if err := rows.Scan(&pid, &pt.ObservedAt, &pt.PricePerTB); err != nil {
			return nil, err
		}
		out[pid] = append(out[pid], pt)
	}
	return out, rows.Err()
}

// Stats returns the dashboard counters in a single round-trip. The previous
// implementation issued 6 sequential queries; the dashboard renders on every
// page load, so batching them keeps the latency low under load.
//
// Each scalar subquery in the SELECT list must return exactly one column.
// The COUNT/MAX pairs for observations and notifications are therefore split
// into two subqueries each — combining them as `(SELECT COUNT(*), MAX(..) FROM ..)`
// was rejected by Postgres with "subquery must return only one column".
func (db *DB) Stats(ctx context.Context) (*Stats, error) {
	s := &Stats{}
	err := db.Pool.QueryRow(ctx, `
SELECT
  (SELECT COUNT(*) FILTER (WHERE enabled)      FROM alerts),
  (SELECT COUNT(*) FILTER (WHERE NOT enabled)  FROM alerts),
  (SELECT COUNT(*)                             FROM products),
  (SELECT COUNT(*)                             FROM price_observations),
  (SELECT MAX(observed_at)                     FROM price_observations),
  (SELECT COUNT(*)                             FROM notifications),
  (SELECT MAX(sent_at)                         FROM notifications),
  (SELECT COUNT(*)                             FROM rejected_deals)
`).Scan(&s.ActiveAlerts, &s.InactiveAlerts, &s.Products,
		&s.Observations, &s.LastObservationAt,
		&s.Notifications, &s.LastNotificationAt,
		&s.RejectedDeals)
	if err != nil {
		return nil, err
	}
	return s, nil
}

func (db *DB) QualityStats(ctx context.Context) (*QualityStats, error) {
	out := &QualityStats{}
	rows, err := db.Pool.Query(ctx, `
WITH product_stats AS (
	SELECT source,
		COUNT(*) AS products,
		COUNT(*) FILTER (WHERE title='') AS missing_title,
		COUNT(*) FILTER (WHERE media_type IS NULL) AS missing_media,
		COUNT(*) FILTER (WHERE drive_category IS NULL) AS missing_category,
		COUNT(*) FILTER (WHERE jsonb_array_length(interfaces)=0) AS missing_interfaces
	FROM products GROUP BY source
),
obs_stats AS (
	SELECT source,
		COUNT(*) AS observations,
		MIN(price_per_tb)::float8 AS min_price_per_tb,
		percentile_cont(0.5) WITHIN GROUP (ORDER BY price_per_tb)::float8 AS median_price_per_tb,
		MAX(price_per_tb)::float8 AS max_price_per_tb
	FROM price_observations
	WHERE price_per_tb > 0 AND quality_score >= 70
	GROUP BY source
),
reject_stats AS (
	SELECT source, COUNT(*) AS rejected FROM rejected_deals GROUP BY source
),
all_sources AS (
	SELECT source FROM product_stats UNION SELECT source FROM obs_stats UNION SELECT source FROM reject_stats
)
SELECT s.source,
	COALESCE(p.products,0), COALESCE(o.observations,0), COALESCE(r.rejected,0),
	COALESCE(p.missing_title,0), COALESCE(p.missing_media,0), COALESCE(p.missing_category,0), COALESCE(p.missing_interfaces,0),
	o.min_price_per_tb, o.median_price_per_tb, o.max_price_per_tb
FROM all_sources s
LEFT JOIN product_stats p ON p.source=s.source
LEFT JOIN obs_stats o ON o.source=s.source
LEFT JOIN reject_stats r ON r.source=s.source
ORDER BY s.source`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var s SourceQuality
		if err := rows.Scan(&s.Source, &s.Products, &s.Observations, &s.Rejected, &s.MissingTitle, &s.MissingMedia, &s.MissingCategory, &s.MissingInterfaces, &s.MinPricePerTB, &s.MedianPricePerTB, &s.MaxPricePerTB); err != nil {
			return nil, err
		}
		out.Sources = append(out.Sources, s)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	reasonRows, err := db.Pool.Query(ctx, `SELECT source, reason, COUNT(*) FROM rejected_deals GROUP BY source, reason ORDER BY COUNT(*) DESC, source, reason LIMIT 20`)
	if err != nil {
		return nil, err
	}
	defer reasonRows.Close()
	for reasonRows.Next() {
		var r RejectReasonStat
		if err := reasonRows.Scan(&r.Source, &r.Reason, &r.Count); err != nil {
			return nil, err
		}
		out.Reasons = append(out.Reasons, r)
	}
	return out, reasonRows.Err()
}

func scanAlerts(rows pgx.Rows, err error) ([]Alert, error) {
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Alert
	for rows.Next() {
		var a Alert
		rows.Scan(&a.ID, &a.Name, &a.MinCapacityTB, &a.MaxCapacityTB, jsonScan(&a.CapacityPresets), jsonScan(&a.Conditions), jsonScan(&a.MediaTypes), jsonScan(&a.DriveCategories), jsonScan(&a.Interfaces), jsonScan(&a.Sources), jsonScan(&a.Brands), jsonScan(&a.Keywords), jsonScan(&a.ExcludeKeywords), jsonScan(&a.RecordingMethods), &a.MaxPricePerTB, &a.MinDiscountPct, &a.CooldownHours, &a.Enabled, &a.DiscordEnabled, &a.CreatedAt, &a.UpdatedAt)
		out = append(out, a)
	}
	return out, nil
}

func ja(v []string) []byte {
	if v == nil {
		v = []string{}
	}
	b, _ := json.Marshal(v)
	return b
}

func jsonScan(target any) any { return &jsw{target} }

type jsw struct{ t any }

func (w *jsw) Scan(src any) error {
	if src == nil {
		return nil
	}
	b, ok := src.([]byte)
	if !ok {
		return nil
	}
	return json.Unmarshal(b, w.t)
}

func ptrStr[T ~string](v *T) *string {
	if v == nil {
		return nil
	}
	s := string(*v)
	return &s
}

func nilIfEmpty(s string) *string {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil
	}
	return &s
}

func validateObservationDeal(deal domain.Deal) error {
	if strings.TrimSpace(deal.Title) == "" {
		return errors.New("cannot record observation with empty title")
	}
	if strings.TrimSpace(deal.URL) == "" {
		return errors.New("cannot record observation with empty URL")
	}
	if deal.CapacityTB <= 0 {
		return errors.New("cannot record observation with invalid capacity")
	}
	if deal.PriceEUR <= 0 {
		return errors.New("cannot record observation with invalid price")
	}
	if deal.PricePerTB <= 0 {
		return errors.New("cannot record observation with invalid price per TB")
	}
	return nil
}

func ifaceStrs(ifs []domain.DriveInterface) []string {
	out := make([]string, len(ifs))
	for i, v := range ifs {
		out[i] = string(v)
	}
	return out
}
