package db

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
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

func (db *DB) Migrate(ctx context.Context) error {
	_, err := db.Pool.Exec(ctx, `
CREATE TABLE IF NOT EXISTS subscribers (chat_id BIGINT PRIMARY KEY, username VARCHAR(255), first_seen_at TIMESTAMPTZ DEFAULT NOW(), last_seen_at TIMESTAMPTZ DEFAULT NOW(), enabled BOOLEAN DEFAULT TRUE);
CREATE TABLE IF NOT EXISTS authorized_users (telegram_user_id BIGINT PRIMARY KEY, label VARCHAR(120) NOT NULL, is_admin BOOLEAN DEFAULT FALSE, enabled BOOLEAN DEFAULT TRUE, created_at TIMESTAMPTZ DEFAULT NOW(), updated_at TIMESTAMPTZ DEFAULT NOW());
CREATE TABLE IF NOT EXISTS alerts (id SERIAL PRIMARY KEY, chat_id BIGINT NOT NULL, owner_user_id BIGINT NOT NULL, name VARCHAR(120) NOT NULL, min_capacity_tb DOUBLE PRECISION, max_capacity_tb DOUBLE PRECISION, capacity_presets JSONB DEFAULT '[]', conditions JSONB DEFAULT '[]', media_types JSONB DEFAULT '[]', drive_categories JSONB DEFAULT '[]', interfaces JSONB DEFAULT '[]', sources JSONB DEFAULT '[]', max_price_per_tb NUMERIC(10,2), min_discount_pct REAL DEFAULT 5.0, cooldown_hours INTEGER DEFAULT 24, enabled BOOLEAN DEFAULT TRUE, created_at TIMESTAMPTZ DEFAULT NOW(), updated_at TIMESTAMPTZ DEFAULT NOW());
CREATE INDEX IF NOT EXISTS idx_alerts_owner ON alerts(owner_user_id);
CREATE TABLE IF NOT EXISTS products (id VARCHAR(80) PRIMARY KEY, source VARCHAR(40) NOT NULL, external_id VARCHAR(255), title TEXT NOT NULL, url TEXT NOT NULL, capacity_tb NUMERIC(10,3) NOT NULL, condition VARCHAR(20), media_type VARCHAR(30), form_factor VARCHAR(120), technology VARCHAR(120), drive_category VARCHAR(40), interfaces JSONB DEFAULT '[]', quality_score INTEGER DEFAULT 0, classification_source VARCHAR(40), canonical_url TEXT, merchant VARCHAR(120), brand VARCHAR(120), model VARCHAR(180), raw_title TEXT, first_seen_at TIMESTAMPTZ DEFAULT NOW(), last_seen_at TIMESTAMPTZ DEFAULT NOW());
CREATE TABLE IF NOT EXISTS price_observations (id SERIAL PRIMARY KEY, product_id VARCHAR(80) REFERENCES products(id), source VARCHAR(40) NOT NULL, observed_at TIMESTAMPTZ DEFAULT NOW(), price_eur NUMERIC(10,2) NOT NULL, price_per_tb NUMERIC(10,2) NOT NULL, quality_score INTEGER DEFAULT 0, raw_json JSONB DEFAULT '{}');
CREATE INDEX IF NOT EXISTS idx_obs_pid ON price_observations(product_id);
CREATE INDEX IF NOT EXISTS idx_obs_ts ON price_observations(observed_at);
CREATE INDEX IF NOT EXISTS idx_obs_latest ON price_observations(product_id, observed_at DESC); -- ⚡ Bolt: composite index to avoid costly sort during DISTINCT ON (product_id) in LatestPrices query
CREATE TABLE IF NOT EXISTS notifications (id SERIAL PRIMARY KEY, alert_id INTEGER REFERENCES alerts(id), product_id VARCHAR(80) REFERENCES products(id), sent_at TIMESTAMPTZ DEFAULT NOW(), price_eur NUMERIC(10,2) NOT NULL, price_per_tb NUMERIC(10,2) NOT NULL, discount_pct NUMERIC(6,2), reason VARCHAR(80) NOT NULL, title TEXT NOT NULL, url TEXT NOT NULL);
CREATE INDEX IF NOT EXISTS idx_notif_aid ON notifications(alert_id);
CREATE INDEX IF NOT EXISTS idx_notif_pid ON notifications(product_id);
CREATE INDEX IF NOT EXISTS idx_notif_ts ON notifications(sent_at);
CREATE TABLE IF NOT EXISTS app_config (key TEXT PRIMARY KEY, value TEXT NOT NULL, updated_at TIMESTAMPTZ DEFAULT NOW());
CREATE TABLE IF NOT EXISTS rejected_deals (id SERIAL PRIMARY KEY, source VARCHAR(40) NOT NULL, reason VARCHAR(80) NOT NULL, detail TEXT, title TEXT, url TEXT, observed_at TIMESTAMPTZ DEFAULT NOW(), raw_json JSONB DEFAULT '{}');
CREATE INDEX IF NOT EXISTS idx_rejected_deals_source ON rejected_deals(source);
CREATE INDEX IF NOT EXISTS idx_rejected_deals_observed ON rejected_deals(observed_at);
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
`)
	return err
}

type Alert struct {
	ID, ChatID, OwnerUserID                                                       int64
	Name                                                                          string
	MinCapacityTB, MaxCapacityTB, MaxPricePerTB                                   *float64
	CapacityPresets, Conditions, MediaTypes, DriveCategories, Interfaces, Sources []string
	Brands, Keywords, ExcludeKeywords, RecordingMethods                           []string
	MinDiscountPct                                                                float64
	CooldownHours                                                                 int
	Enabled                                                                       bool
	CreatedAt, UpdatedAt                                                          time.Time
}
type Product struct {
	ID, Source, Title, URL                                                  string
	ExternalID, Condition, MediaType, FormFactor, Technology, DriveCategory *string
	ClassificationSource, CanonicalURL, Merchant, Brand, Model, RawTitle    *string
	RecordingMethod                                                         *string
	CapacityTB                                                              float64
	Interfaces                                                              []string
	QualityScore                                                            int
	FirstSeenAt, LastSeenAt                                                 time.Time
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
	ID, AlertID                   int64
	ProductID, Reason, Title, URL string
	SentAt                        time.Time
	PriceEUR, PricePerTB          float64
	DiscountPct                   *float64
}
type AuthorizedUser struct {
	TelegramUserID       int64
	Label                string
	IsAdmin, Enabled     bool
	CreatedAt, UpdatedAt time.Time
}
type CurrentPrice struct {
	ProductID            string
	Source, Title, URL   string
	MediaType            *string
	CapacityTB           float64
	PriceEUR, PricePerTB float64
	ObservedAt           time.Time
}
type Stats struct {
	ActiveAlerts, InactiveAlerts          int64
	AuthorizedEnabled, AuthorizedDisabled int64
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

func (db *DB) UpsertSubscriber(ctx context.Context, chatID int64, username *string) error {
	_, err := db.Pool.Exec(ctx, `INSERT INTO subscribers (chat_id, username) VALUES ($1,$2) ON CONFLICT(chat_id) DO UPDATE SET username=$2, last_seen_at=NOW(), enabled=TRUE`, chatID, username)
	return err
}
func (db *DB) IsUserAllowed(ctx context.Context, uid int64) (bool, error) {
	var e bool
	err := db.Pool.QueryRow(ctx, `SELECT enabled FROM authorized_users WHERE telegram_user_id=$1`, uid).Scan(&e)
	if err == pgx.ErrNoRows {
		return false, nil
	}
	return e, err
}
func (db *DB) UpsertAuthorizedUser(ctx context.Context, uid int64, label string, enabled bool) error {
	if label == "" {
		label = fmt.Sprintf("%d", uid)
	}
	_, err := db.Pool.Exec(ctx, `INSERT INTO authorized_users (telegram_user_id,label,is_admin,enabled) VALUES ($1,$2,FALSE,$3) ON CONFLICT(telegram_user_id) DO UPDATE SET label=$2,is_admin=FALSE,enabled=$3,updated_at=NOW()`, uid, label, enabled)
	return err
}
func (db *DB) SetAuthorizedUserEnabled(ctx context.Context, uid int64, enabled bool) error {
	_, err := db.Pool.Exec(ctx, `UPDATE authorized_users SET enabled=$1, updated_at=NOW() WHERE telegram_user_id=$2`, enabled, uid)
	return err
}

// AlertDraft carries all filter slices for alert creation in a single value,
// keeping the CreateAlert signature readable as new filter dimensions are added.
type AlertDraft struct {
	CapacityPresets, Conditions, MediaTypes, DriveCategories, Interfaces, Sources []string
	Brands, Keywords, ExcludeKeywords, RecordingMethods                           []string
	MaxPricePerTB                                                                  *float64
	MinDiscountPct                                                                 float64
	CooldownHours                                                                  int
}

func (db *DB) CreateAlert(ctx context.Context, chatID, ownerID int64, name string, d AlertDraft) (*Alert, error) {
	a := &Alert{ChatID: chatID, OwnerUserID: ownerID, Name: name, MaxPricePerTB: d.MaxPricePerTB, MinDiscountPct: d.MinDiscountPct, CooldownHours: d.CooldownHours, CapacityPresets: d.CapacityPresets, Conditions: d.Conditions, MediaTypes: d.MediaTypes, DriveCategories: d.DriveCategories, Interfaces: d.Interfaces, Sources: d.Sources, Brands: d.Brands, Keywords: d.Keywords, ExcludeKeywords: d.ExcludeKeywords, RecordingMethods: d.RecordingMethods, Enabled: true, CreatedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC()}
	err := db.Pool.QueryRow(ctx, `INSERT INTO alerts (chat_id,owner_user_id,name,capacity_presets,conditions,media_types,drive_categories,interfaces,sources,brands,keywords,exclude_keywords,recording_methods,max_price_per_tb,min_discount_pct,cooldown_hours) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18) RETURNING id`,
		chatID, ownerID, name, ja(d.CapacityPresets), ja(d.Conditions), ja(d.MediaTypes), ja(d.DriveCategories), ja(d.Interfaces), ja(d.Sources), ja(d.Brands), ja(d.Keywords), ja(d.ExcludeKeywords), ja(d.RecordingMethods), d.MaxPricePerTB, d.MinDiscountPct, d.CooldownHours).Scan(&a.ID)
	return a, err
}

func (db *DB) ListAlerts(ctx context.Context, onlyEnabled bool) ([]Alert, error) {
	q := "SELECT id,chat_id,owner_user_id,name,min_capacity_tb,max_capacity_tb,capacity_presets,conditions,media_types,drive_categories,interfaces,sources,brands,keywords,exclude_keywords,recording_methods,max_price_per_tb,min_discount_pct,cooldown_hours,enabled,created_at,updated_at FROM alerts"
	if onlyEnabled {
		q += " WHERE enabled=TRUE"
	}
	q += " ORDER BY id"
	return scanAlerts(db.Pool.Query(ctx, q))
}

func (db *DB) GetAlertsByOwner(ctx context.Context, ownerID int64, onlyEnabled bool) ([]Alert, error) {
	q := "SELECT id,chat_id,owner_user_id,name,min_capacity_tb,max_capacity_tb,capacity_presets,conditions,media_types,drive_categories,interfaces,sources,brands,keywords,exclude_keywords,recording_methods,max_price_per_tb,min_discount_pct,cooldown_hours,enabled,created_at,updated_at FROM alerts WHERE owner_user_id=$1"
	if onlyEnabled {
		q += " AND enabled=TRUE"
	}
	q += " ORDER BY id"
	return scanAlerts(db.Pool.Query(ctx, q, ownerID))
}

func (db *DB) GetAlert(ctx context.Context, ownerID, aID int64) (*Alert, error) {
	a := &Alert{}
	err := db.Pool.QueryRow(ctx, "SELECT id,chat_id,owner_user_id,name,min_capacity_tb,max_capacity_tb,capacity_presets,conditions,media_types,drive_categories,interfaces,sources,brands,keywords,exclude_keywords,recording_methods,max_price_per_tb,min_discount_pct,cooldown_hours,enabled,created_at,updated_at FROM alerts WHERE owner_user_id=$1 AND id=$2", ownerID, aID).Scan(
		&a.ID, &a.ChatID, &a.OwnerUserID, &a.Name, &a.MinCapacityTB, &a.MaxCapacityTB, jsonScan(&a.CapacityPresets), jsonScan(&a.Conditions), jsonScan(&a.MediaTypes), jsonScan(&a.DriveCategories), jsonScan(&a.Interfaces), jsonScan(&a.Sources), jsonScan(&a.Brands), jsonScan(&a.Keywords), jsonScan(&a.ExcludeKeywords), jsonScan(&a.RecordingMethods), &a.MaxPricePerTB, &a.MinDiscountPct, &a.CooldownHours, &a.Enabled, &a.CreatedAt, &a.UpdatedAt)
	if err == pgx.ErrNoRows {
		return nil, nil
	}
	return a, err
}

func (db *DB) SetAlertEnabled(ctx context.Context, ownerID, aID int64, enabled bool) error {
	_, err := db.Pool.Exec(ctx, "UPDATE alerts SET enabled=$1, updated_at=NOW() WHERE owner_user_id=$2 AND id=$3", enabled, ownerID, aID)
	return err
}

func (db *DB) DeleteAlert(ctx context.Context, ownerID, aID int64) error {
	_, err := db.Pool.Exec(ctx, "DELETE FROM alerts WHERE owner_user_id=$1 AND id=$2", ownerID, aID)
	return err
}

func (db *DB) UpsertProduct(ctx context.Context, deal domain.Deal) error {
	ifaces := ifaceStrs(deal.Interfaces)
	_, err := db.Pool.Exec(ctx, `INSERT INTO products(id,source,external_id,title,url,capacity_tb,condition,media_type,form_factor,technology,drive_category,interfaces,quality_score,classification_source,canonical_url,merchant,brand,model,raw_title,recording_method) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18,$19,$20) ON CONFLICT(id) DO UPDATE SET title=$4,url=$5,capacity_tb=$6,condition=$7,media_type=$8,form_factor=$9,technology=$10,drive_category=$11,interfaces=$12,quality_score=$13,classification_source=$14,canonical_url=$15,merchant=$16,brand=$17,model=$18,raw_title=$19,recording_method=$20,last_seen_at=NOW()`,
		deal.ProductID(), deal.Source, deal.ExternalID, deal.Title, deal.URL, deal.CapacityTB, ptrStr(deal.Condition), ptrStr(deal.MediaType), deal.FormFactor, deal.Technology, ptrStr(deal.DriveCategory), ja(ifaces), deal.QualityScore, nilIfEmpty(deal.ClassificationSource), nilIfEmpty(deal.CanonicalURL), deal.Merchant, deal.Brand, deal.Model, nilIfEmpty(deal.RawTitle), ptrStr(deal.RecordingMethod))
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
	_, err := tx.Exec(ctx, `INSERT INTO products(id,source,external_id,title,url,capacity_tb,condition,media_type,form_factor,technology,drive_category,interfaces,quality_score,classification_source,canonical_url,merchant,brand,model,raw_title,recording_method) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18,$19,$20) ON CONFLICT(id) DO UPDATE SET title=$4,url=$5,capacity_tb=$6,condition=$7,media_type=$8,form_factor=$9,technology=$10,drive_category=$11,interfaces=$12,quality_score=$13,classification_source=$14,canonical_url=$15,merchant=$16,brand=$17,model=$18,raw_title=$19,recording_method=$20,last_seen_at=NOW()`,
		deal.ProductID(), deal.Source, deal.ExternalID, deal.Title, deal.URL, deal.CapacityTB, ptrStr(deal.Condition), ptrStr(deal.MediaType), deal.FormFactor, deal.Technology, ptrStr(deal.DriveCategory), ja(ifaces), deal.QualityScore, nilIfEmpty(deal.ClassificationSource), nilIfEmpty(deal.CanonicalURL), deal.Merchant, deal.Brand, deal.Model, nilIfEmpty(deal.RawTitle), ptrStr(deal.RecordingMethod))
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

func (db *DB) LastNotification(ctx context.Context, aID int64, pid string) (*Notification, error) {
	n := &Notification{}
	err := db.Pool.QueryRow(ctx, "SELECT id,alert_id,product_id,sent_at,price_eur,price_per_tb,discount_pct,reason,title,url FROM notifications WHERE alert_id=$1 AND product_id=$2 ORDER BY sent_at DESC LIMIT 1", aID, pid).Scan(&n.ID, &n.AlertID, &n.ProductID, &n.SentAt, &n.PriceEUR, &n.PricePerTB, &n.DiscountPct, &n.Reason, &n.Title, &n.URL)
	if err == pgx.ErrNoRows {
		return nil, nil
	}
	return n, err
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
	_, err := tx.Exec(ctx, `INSERT INTO products(id,source,external_id,title,url,capacity_tb,condition,media_type,form_factor,technology,drive_category,interfaces,quality_score,classification_source,canonical_url,merchant,brand,model,raw_title,recording_method) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18,$19,$20) ON CONFLICT(id) DO UPDATE SET title=$4,url=$5,capacity_tb=$6,condition=$7,media_type=$8,form_factor=$9,technology=$10,drive_category=$11,interfaces=$12,quality_score=$13,classification_source=$14,canonical_url=$15,merchant=$16,brand=$17,model=$18,raw_title=$19,recording_method=$20,last_seen_at=NOW()`,
		deal.ProductID(), deal.Source, deal.ExternalID, deal.Title, deal.URL, deal.CapacityTB, ptrStr(deal.Condition), ptrStr(deal.MediaType), deal.FormFactor, deal.Technology, ptrStr(deal.DriveCategory), ja(ifaces), deal.QualityScore, nilIfEmpty(deal.ClassificationSource), nilIfEmpty(deal.CanonicalURL), deal.Merchant, deal.Brand, deal.Model, nilIfEmpty(deal.RawTitle), ptrStr(deal.RecordingMethod))
	if err != nil {
		return err
	}
	_, err = tx.Exec(ctx, `INSERT INTO notifications(alert_id,product_id,price_eur,price_per_tb,discount_pct,reason,title,url) VALUES($1,$2,$3,$4,$5,$6,$7,$8)`, alert.ID, deal.ProductID(), deal.PriceEUR, deal.PricePerTB, disc, reason, deal.Title, deal.URL)
	if err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func (db *DB) ToggleAlertFilter(ctx context.Context, ownerID, aID int64, field, value string) error {
	m := map[string][]string{"condition": nil, "media": nil, "category": nil, "interface": nil, "source": nil, "brand": nil, "recording_method": nil}
	a, err := db.GetAlert(ctx, ownerID, aID)
	if err != nil || a == nil {
		return err
	}
	switch field {
	case "condition":
		m[field] = a.Conditions
	case "media":
		m[field] = a.MediaTypes
	case "category":
		m[field] = a.DriveCategories
	case "interface":
		m[field] = a.Interfaces
	case "source":
		m[field] = a.Sources
	case "brand":
		m[field] = a.Brands
	case "recording_method":
		m[field] = a.RecordingMethods
	default:
		return fmt.Errorf("invalid field")
	}
	vals := m[field]
	found := -1
	for i, v := range vals {
		if v == value {
			found = i
			break
		}
	}
	if found >= 0 {
		vals = append(vals[:found], vals[found+1:]...)
	} else {
		vals = append(vals, value)
	}
	cols := map[string]string{"condition": "conditions", "media": "media_types", "category": "drive_categories", "interface": "interfaces", "source": "sources", "brand": "brands", "recording_method": "recording_methods"}
	_, err = db.Pool.Exec(ctx, fmt.Sprintf("UPDATE alerts SET %s=$1, updated_at=NOW() WHERE owner_user_id=$2 AND id=$3", cols[field]), ja(vals), ownerID, aID)
	return err
}

// SetAlertKeywords replaces the keyword and exclude-keyword lists of an alert.
// Either slice may be nil to clear that list.
func (db *DB) SetAlertKeywords(ctx context.Context, ownerID, aID int64, keywords, excludeKeywords []string) error {
	_, err := db.Pool.Exec(ctx, "UPDATE alerts SET keywords=$1, exclude_keywords=$2, updated_at=NOW() WHERE owner_user_id=$3 AND id=$4", ja(keywords), ja(excludeKeywords), ownerID, aID)
	return err
}

func (db *DB) SetAlertMaxPrice(ctx context.Context, ownerID, aID int64, price *float64) error {
	_, err := db.Pool.Exec(ctx, "UPDATE alerts SET max_price_per_tb=$1, updated_at=NOW() WHERE owner_user_id=$2 AND id=$3", price, ownerID, aID)
	return err
}

func (db *DB) UpdateAlertCaps(ctx context.Context, ownerID, aID int64, presets []string) error {
	_, err := db.Pool.Exec(ctx, "UPDATE alerts SET capacity_presets=$1, min_capacity_tb=NULL, max_capacity_tb=NULL, updated_at=NOW() WHERE owner_user_id=$2 AND id=$3", ja(presets), ownerID, aID)
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
SELECT l.product_id, l.source, p.title, p.url, p.media_type, p.capacity_tb, l.price_eur, l.price_per_tb, l.observed_at
FROM latest l
JOIN products p ON p.id = l.product_id
WHERE p.quality_score >= 50
ORDER BY l.price_per_tb ASC, l.observed_at DESC
LIMIT $1`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []CurrentPrice
	for rows.Next() {
		var p CurrentPrice
		if err := rows.Scan(&p.ProductID, &p.Source, &p.Title, &p.URL, &p.MediaType, &p.CapacityTB, &p.PriceEUR, &p.PricePerTB, &p.ObservedAt); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

// GetProduct fetches a single product by its ID for the detail page.
func (db *DB) GetProduct(ctx context.Context, productID string) (*Product, error) {
	p := &Product{}
	err := db.Pool.QueryRow(ctx, `SELECT id,source,external_id,title,url,capacity_tb,condition,media_type,form_factor,technology,drive_category,interfaces,quality_score,classification_source,canonical_url,merchant,brand,model,raw_title,recording_method,first_seen_at,last_seen_at FROM products WHERE id=$1`, productID).Scan(
		&p.ID, &p.Source, &p.ExternalID, &p.Title, &p.URL, &p.CapacityTB, &p.Condition, &p.MediaType, &p.FormFactor, &p.Technology, &p.DriveCategory, jsonScan(&p.Interfaces), &p.QualityScore, &p.ClassificationSource, &p.CanonicalURL, &p.Merchant, &p.Brand, &p.Model, &p.RawTitle, &p.RecordingMethod, &p.FirstSeenAt, &p.LastSeenAt)
	if err == pgx.ErrNoRows {
		return nil, nil
	}
	return p, err
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

func (db *DB) ListAuthorizedUsers(ctx context.Context, includeDisabled bool) ([]AuthorizedUser, error) {
	q := "SELECT telegram_user_id,label,is_admin,enabled,created_at,updated_at FROM authorized_users"
	if !includeDisabled {
		q += " WHERE enabled=TRUE"
	}
	q += " ORDER BY label"
	rows, err := db.Pool.Query(ctx, q)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var users []AuthorizedUser
	for rows.Next() {
		var u AuthorizedUser
		rows.Scan(&u.TelegramUserID, &u.Label, &u.IsAdmin, &u.Enabled, &u.CreatedAt, &u.UpdatedAt)
		users = append(users, u)
	}
	return users, nil
}

func (db *DB) Stats(ctx context.Context) (*Stats, error) {
	s := &Stats{}
	err := db.Pool.QueryRow(ctx, `SELECT COUNT(*) FILTER (WHERE enabled), COUNT(*) FILTER (WHERE NOT enabled) FROM alerts`).Scan(&s.ActiveAlerts, &s.InactiveAlerts)
	if err != nil {
		return nil, err
	}
	err = db.Pool.QueryRow(ctx, `SELECT COUNT(*) FILTER (WHERE enabled), COUNT(*) FILTER (WHERE NOT enabled) FROM authorized_users`).Scan(&s.AuthorizedEnabled, &s.AuthorizedDisabled)
	if err != nil {
		return nil, err
	}
	err = db.Pool.QueryRow(ctx, `SELECT COUNT(*) FROM products`).Scan(&s.Products)
	if err != nil {
		return nil, err
	}
	err = db.Pool.QueryRow(ctx, `SELECT COUNT(*), MAX(observed_at) FROM price_observations`).Scan(&s.Observations, &s.LastObservationAt)
	if err != nil {
		return nil, err
	}
	err = db.Pool.QueryRow(ctx, `SELECT COUNT(*), MAX(sent_at) FROM notifications`).Scan(&s.Notifications, &s.LastNotificationAt)
	if err != nil {
		return nil, err
	}
	err = db.Pool.QueryRow(ctx, `SELECT COUNT(*) FROM rejected_deals`).Scan(&s.RejectedDeals)
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
		rows.Scan(&a.ID, &a.ChatID, &a.OwnerUserID, &a.Name, &a.MinCapacityTB, &a.MaxCapacityTB, jsonScan(&a.CapacityPresets), jsonScan(&a.Conditions), jsonScan(&a.MediaTypes), jsonScan(&a.DriveCategories), jsonScan(&a.Interfaces), jsonScan(&a.Sources), jsonScan(&a.Brands), jsonScan(&a.Keywords), jsonScan(&a.ExcludeKeywords), jsonScan(&a.RecordingMethods), &a.MaxPricePerTB, &a.MinDiscountPct, &a.CooldownHours, &a.Enabled, &a.CreatedAt, &a.UpdatedAt)
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
