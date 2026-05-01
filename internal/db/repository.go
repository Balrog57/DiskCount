package db

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"sort"
	"time"

	"github.com/MarcPartensky/DiskCount/internal/domain"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type DB struct{ Pool *pgxpool.Pool }

func New(ctx context.Context, databaseURL string) (*DB, error) {
	cfg, err := pgxpool.ParseConfig(databaseURL)
	if err != nil { return nil, fmt.Errorf("parse: %w", err) }
	p, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil { return nil, fmt.Errorf("connect: %w", err) }
	if err := p.Ping(ctx); err != nil { return nil, fmt.Errorf("ping: %w", err) }
	return &DB{Pool: p}, nil
}

func (db *DB) Close() { db.Pool.Close() }

func (db *DB) Migrate(ctx context.Context) error {
	_, err := db.Pool.Exec(ctx, `
CREATE TABLE IF NOT EXISTS subscribers (chat_id BIGINT PRIMARY KEY, username VARCHAR(255), first_seen_at TIMESTAMPTZ DEFAULT NOW(), last_seen_at TIMESTAMPTZ DEFAULT NOW(), enabled BOOLEAN DEFAULT TRUE);
CREATE TABLE IF NOT EXISTS authorized_users (telegram_user_id BIGINT PRIMARY KEY, label VARCHAR(120) NOT NULL, is_admin BOOLEAN DEFAULT FALSE, enabled BOOLEAN DEFAULT TRUE, created_at TIMESTAMPTZ DEFAULT NOW(), updated_at TIMESTAMPTZ DEFAULT NOW());
CREATE TABLE IF NOT EXISTS alerts (id SERIAL PRIMARY KEY, chat_id BIGINT NOT NULL, owner_user_id BIGINT NOT NULL, name VARCHAR(120) NOT NULL, min_capacity_tb DOUBLE PRECISION, max_capacity_tb DOUBLE PRECISION, capacity_presets JSONB DEFAULT '[]', conditions JSONB DEFAULT '[]', media_types JSONB DEFAULT '[]', drive_categories JSONB DEFAULT '[]', interfaces JSONB DEFAULT '[]', sources JSONB DEFAULT '[]', max_price_per_tb NUMERIC(10,2), min_discount_pct REAL DEFAULT 5.0, cooldown_hours INTEGER DEFAULT 24, enabled BOOLEAN DEFAULT TRUE, created_at TIMESTAMPTZ DEFAULT NOW(), updated_at TIMESTAMPTZ DEFAULT NOW());
CREATE INDEX IF NOT EXISTS idx_alerts_owner ON alerts(owner_user_id);
CREATE TABLE IF NOT EXISTS products (id VARCHAR(80) PRIMARY KEY, source VARCHAR(40) NOT NULL, external_id VARCHAR(255), title TEXT NOT NULL, url TEXT NOT NULL, capacity_tb NUMERIC(10,3) NOT NULL, condition VARCHAR(20), media_type VARCHAR(30), form_factor VARCHAR(120), technology VARCHAR(120), drive_category VARCHAR(40), interfaces JSONB DEFAULT '[]', first_seen_at TIMESTAMPTZ DEFAULT NOW(), last_seen_at TIMESTAMPTZ DEFAULT NOW());
CREATE TABLE IF NOT EXISTS price_observations (id SERIAL PRIMARY KEY, product_id VARCHAR(80) REFERENCES products(id), source VARCHAR(40) NOT NULL, observed_at TIMESTAMPTZ DEFAULT NOW(), price_eur NUMERIC(10,2) NOT NULL, price_per_tb NUMERIC(10,2) NOT NULL, raw_json JSONB DEFAULT '{}');
CREATE INDEX IF NOT EXISTS idx_obs_pid ON price_observations(product_id);
CREATE INDEX IF NOT EXISTS idx_obs_ts ON price_observations(observed_at);
CREATE TABLE IF NOT EXISTS notifications (id SERIAL PRIMARY KEY, alert_id INTEGER REFERENCES alerts(id), product_id VARCHAR(80) REFERENCES products(id), sent_at TIMESTAMPTZ DEFAULT NOW(), price_eur NUMERIC(10,2) NOT NULL, price_per_tb NUMERIC(10,2) NOT NULL, discount_pct NUMERIC(6,2), reason VARCHAR(80) NOT NULL, title TEXT NOT NULL, url TEXT NOT NULL);
CREATE INDEX IF NOT EXISTS idx_notif_aid ON notifications(alert_id);
CREATE INDEX IF NOT EXISTS idx_notif_pid ON notifications(product_id);
`)
	return err
}

type Alert struct {
	ID, ChatID, OwnerUserID int64
	Name string; MinCapacityTB, MaxCapacityTB, MaxPricePerTB *float64
	CapacityPresets, Conditions, MediaTypes, DriveCategories, Interfaces, Sources []string
	MinDiscountPct float64; CooldownHours int; Enabled bool; CreatedAt, UpdatedAt time.Time
}
type Product struct {
	ID, Source, Title, URL string; ExternalID, Condition, MediaType, FormFactor, Technology, DriveCategory *string
	CapacityTB float64; Interfaces []string; FirstSeenAt, LastSeenAt time.Time
}
type PriceObservation struct {
	ID int64; ProductID, Source string; ObservedAt time.Time; PriceEUR, PricePerTB float64; RawJSON string
}
type Notification struct {
	ID, AlertID int64; ProductID, Reason, Title, URL string; SentAt time.Time; PriceEUR, PricePerTB float64; DiscountPct *float64
}
type AuthorizedUser struct {
	TelegramUserID int64; Label string; IsAdmin, Enabled bool; CreatedAt, UpdatedAt time.Time
}

func (db *DB) UpsertSubscriber(ctx context.Context, chatID int64, username *string) error {
	_, err := db.Pool.Exec(ctx, `INSERT INTO subscribers (chat_id, username) VALUES ($1,$2) ON CONFLICT(chat_id) DO UPDATE SET username=$2, last_seen_at=NOW(), enabled=TRUE`, chatID, username)
	return err
}
func (db *DB) IsUserAllowed(ctx context.Context, uid int64) (bool, error) {
	var e bool; err := db.Pool.QueryRow(ctx, `SELECT enabled FROM authorized_users WHERE telegram_user_id=$1`, uid).Scan(&e)
	if err == pgx.ErrNoRows { return false, nil }
	return e, err
}

func (db *DB) CreateAlert(ctx context.Context, chatID, ownerID int64, name string, maxPrice *float64, minDisc float64, cooldown int, caps, conds, medias, cats, ifaces, srcs []string) (*Alert, error) {
	a := &Alert{ChatID: chatID, OwnerUserID: ownerID, Name: name, MaxPricePerTB: maxPrice, MinDiscountPct: minDisc, CooldownHours: cooldown, CapacityPresets: caps, Conditions: conds, MediaTypes: medias, DriveCategories: cats, Interfaces: ifaces, Sources: srcs, Enabled: true, CreatedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC()}
	err := db.Pool.QueryRow(ctx, `INSERT INTO alerts (chat_id,owner_user_id,name,capacity_presets,conditions,media_types,drive_categories,interfaces,sources,max_price_per_tb,min_discount_pct,cooldown_hours) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12) RETURNING id`,
		chatID, ownerID, name, ja(caps), ja(conds), ja(medias), ja(cats), ja(ifaces), ja(srcs), maxPrice, minDisc, cooldown).Scan(&a.ID)
	return a, err
}

func (db *DB) ListAlerts(ctx context.Context, onlyEnabled bool) ([]Alert, error) {
	q := "SELECT id,chat_id,owner_user_id,name,min_capacity_tb,max_capacity_tb,capacity_presets,conditions,media_types,drive_categories,interfaces,sources,max_price_per_tb,min_discount_pct,cooldown_hours,enabled,created_at,updated_at FROM alerts"
	if onlyEnabled { q += " WHERE enabled=TRUE" }; q += " ORDER BY id"
	return scanAlerts(db.Pool.Query(ctx, q))
}

func (db *DB) GetAlertsByOwner(ctx context.Context, ownerID int64, onlyEnabled bool) ([]Alert, error) {
	q := "SELECT id,chat_id,owner_user_id,name,min_capacity_tb,max_capacity_tb,capacity_presets,conditions,media_types,drive_categories,interfaces,sources,max_price_per_tb,min_discount_pct,cooldown_hours,enabled,created_at,updated_at FROM alerts WHERE owner_user_id=$1"
	if onlyEnabled { q += " AND enabled=TRUE" }; q += " ORDER BY id"
	return scanAlerts(db.Pool.Query(ctx, q, ownerID))
}

func (db *DB) GetAlert(ctx context.Context, ownerID, aID int64) (*Alert, error) {
	a := &Alert{}
	err := db.Pool.QueryRow(ctx, "SELECT id,chat_id,owner_user_id,name,min_capacity_tb,max_capacity_tb,capacity_presets,conditions,media_types,drive_categories,interfaces,sources,max_price_per_tb,min_discount_pct,cooldown_hours,enabled,created_at,updated_at FROM alerts WHERE owner_user_id=$1 AND id=$2", ownerID, aID).Scan(
		&a.ID, &a.ChatID, &a.OwnerUserID, &a.Name, &a.MinCapacityTB, &a.MaxCapacityTB, jsonScan(&a.CapacityPresets), jsonScan(&a.Conditions), jsonScan(&a.MediaTypes), jsonScan(&a.DriveCategories), jsonScan(&a.Interfaces), jsonScan(&a.Sources), &a.MaxPricePerTB, &a.MinDiscountPct, &a.CooldownHours, &a.Enabled, &a.CreatedAt, &a.UpdatedAt)
	if err == pgx.ErrNoRows { return nil, nil }
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
	_, err := db.Pool.Exec(ctx, `INSERT INTO products(id,source,external_id,title,url,capacity_tb,condition,media_type,form_factor,technology,drive_category,interfaces) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12) ON CONFLICT(id) DO UPDATE SET title=$4,url=$5,capacity_tb=$6,condition=$7,media_type=$8,form_factor=$9,technology=$10,drive_category=$11,interfaces=$12,last_seen_at=NOW()`,
		deal.ProductID(), deal.Source, deal.ExternalID, deal.Title, deal.URL, deal.CapacityTB, ptrStr(deal.Condition), ptrStr(deal.MediaType), deal.FormFactor, deal.Technology, ptrStr(deal.DriveCategory), ja(ifaces))
	return err
}

func (db *DB) RecordObservation(ctx context.Context, deal domain.Deal) error {
	tx, _ := db.Pool.Begin(ctx)
	if tx == nil { return fmt.Errorf("tx begin failed") }
	defer tx.Rollback(ctx)
	ifaces := ifaceStrs(deal.Interfaces)
	_, err := tx.Exec(ctx, `INSERT INTO products(id,source,external_id,title,url,capacity_tb,condition,media_type,form_factor,technology,drive_category,interfaces) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12) ON CONFLICT(id) DO UPDATE SET title=$4,url=$5,capacity_tb=$6,condition=$7,media_type=$8,form_factor=$9,technology=$10,drive_category=$11,interfaces=$12,last_seen_at=NOW()`,
		deal.ProductID(), deal.Source, deal.ExternalID, deal.Title, deal.URL, deal.CapacityTB, ptrStr(deal.Condition), ptrStr(deal.MediaType), deal.FormFactor, deal.Technology, ptrStr(deal.DriveCategory), ja(ifaces))
	if err != nil { return err }
	raw, _ := json.Marshal(deal.Raw)
	obs := deal.ObservedAt; if obs.IsZero() { obs = time.Now().UTC() }
	_, err = tx.Exec(ctx, `INSERT INTO price_observations(product_id,source,observed_at,price_eur,price_per_tb,raw_json) VALUES($1,$2,$3,$4,$5,$6)`, deal.ProductID(), deal.Source, obs, deal.PriceEUR, deal.PricePerTB, raw)
	if err != nil { return err }
	return tx.Commit(ctx)
}

func (db *DB) BaselinePricePerTB(ctx context.Context, pid string, before time.Time, days int) (*float64, error) {
	start := before.Add(-time.Duration(days) * 24 * time.Hour)
	rows, err := db.Pool.Query(ctx, "SELECT price_per_tb FROM price_observations WHERE product_id=$1 AND observed_at>=$2 AND observed_at<$3", pid, start, before)
	if err != nil { return nil, err }
	defer rows.Close()
	var vals []float64
	for rows.Next() { var v float64; rows.Scan(&v); vals = append(vals, v) }
	if len(vals) == 0 { return nil, nil }
	sort.Float64s(vals)
	m := len(vals) / 2
	var med float64
	if len(vals)%2 == 0 { med = (vals[m-1] + vals[m]) / 2 } else { med = vals[m] }
	r := math.Round(med*100) / 100
	return &r, nil
}

func (db *DB) LastNotification(ctx context.Context, aID int64, pid string) (*Notification, error) {
	n := &Notification{}
	err := db.Pool.QueryRow(ctx, "SELECT id,alert_id,product_id,sent_at,price_eur,price_per_tb,discount_pct,reason,title,url FROM notifications WHERE alert_id=$1 AND product_id=$2 ORDER BY sent_at DESC LIMIT 1", aID, pid).Scan(&n.ID, &n.AlertID, &n.ProductID, &n.SentAt, &n.PriceEUR, &n.PricePerTB, &n.DiscountPct, &n.Reason, &n.Title, &n.URL)
	if err == pgx.ErrNoRows { return nil, nil }
	return n, err
}

func (db *DB) RecordNotification(ctx context.Context, alert *Alert, deal domain.Deal, reason string, disc *float64) error {
	tx, _ := db.Pool.Begin(ctx)
	if tx == nil { return fmt.Errorf("tx begin") }
	defer tx.Rollback(ctx)
	ifaces := ifaceStrs(deal.Interfaces)
	_, err := tx.Exec(ctx, `INSERT INTO products(id,source,external_id,title,url,capacity_tb,condition,media_type,form_factor,technology,drive_category,interfaces) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12) ON CONFLICT(id) DO UPDATE SET title=$4,url=$5,capacity_tb=$6,condition=$7,media_type=$8,form_factor=$9,technology=$10,drive_category=$11,interfaces=$12,last_seen_at=NOW()`,
		deal.ProductID(), deal.Source, deal.ExternalID, deal.Title, deal.URL, deal.CapacityTB, ptrStr(deal.Condition), ptrStr(deal.MediaType), deal.FormFactor, deal.Technology, ptrStr(deal.DriveCategory), ja(ifaces))
	if err != nil { return err }
	_, err = tx.Exec(ctx, `INSERT INTO notifications(alert_id,product_id,price_eur,price_per_tb,discount_pct,reason,title,url) VALUES($1,$2,$3,$4,$5,$6,$7,$8)`, alert.ID, deal.ProductID(), deal.PriceEUR, deal.PricePerTB, disc, reason, deal.Title, deal.URL)
	if err != nil { return err }
	return tx.Commit(ctx)
}

func (db *DB) ToggleAlertFilter(ctx context.Context, ownerID, aID int64, field, value string) error {
	m := map[string][]string{"condition": nil, "media": nil, "category": nil, "interface": nil, "source": nil}
	a, err := db.GetAlert(ctx, ownerID, aID)
	if err != nil || a == nil { return err }
	switch field {
	case "condition": m[field] = a.Conditions
	case "media": m[field] = a.MediaTypes
	case "category": m[field] = a.DriveCategories
	case "interface": m[field] = a.Interfaces
	case "source": m[field] = a.Sources
	default: return fmt.Errorf("invalid field")
	}
	vals := m[field]
	found := -1
	for i, v := range vals { if v == value { found = i; break } }
	if found >= 0 { vals = append(vals[:found], vals[found+1:]...) } else { vals = append(vals, value) }
	cols := map[string]string{"condition": "conditions", "media": "media_types", "category": "drive_categories", "interface": "interfaces", "source": "sources"}
	_, err = db.Pool.Exec(ctx, fmt.Sprintf("UPDATE alerts SET %s=$1, updated_at=NOW() WHERE owner_user_id=$2 AND id=$3", cols[field]), ja(vals), ownerID, aID)
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

func (db *DB) ListAuthorizedUsers(ctx context.Context, includeDisabled bool) ([]AuthorizedUser, error) {
	rows, err := db.Pool.Query(ctx, "SELECT telegram_user_id,label,is_admin,enabled,created_at,updated_at FROM authorized_users ORDER BY label")
	if err != nil { return nil, err }
	defer rows.Close()
	var users []AuthorizedUser
	for rows.Next() {
		var u AuthorizedUser
		rows.Scan(&u.TelegramUserID, &u.Label, &u.IsAdmin, &u.Enabled, &u.CreatedAt, &u.UpdatedAt)
		users = append(users, u)
	}
	return users, nil
}

func scanAlerts(rows pgx.Rows, err error) ([]Alert, error) {
	if err != nil { return nil, err }
	defer rows.Close()
	var out []Alert
	for rows.Next() {
		var a Alert
		rows.Scan(&a.ID, &a.ChatID, &a.OwnerUserID, &a.Name, &a.MinCapacityTB, &a.MaxCapacityTB, jsonScan(&a.CapacityPresets), jsonScan(&a.Conditions), jsonScan(&a.MediaTypes), jsonScan(&a.DriveCategories), jsonScan(&a.Interfaces), jsonScan(&a.Sources), &a.MaxPricePerTB, &a.MinDiscountPct, &a.CooldownHours, &a.Enabled, &a.CreatedAt, &a.UpdatedAt)
		out = append(out, a)
	}
	return out, nil
}

func ja(v []string) []byte { if v == nil { v = []string{} }; b, _ := json.Marshal(v); return b }

func jsonScan(target any) any { return &jsw{target} }
type jsw struct{ t any }
func (w *jsw) Scan(src any) error {
	if src == nil { return nil }
	b, ok := src.([]byte); if !ok { return nil }
	return json.Unmarshal(b, w.t)
}

func ptrStr[T ~string](v *T) *string {
	if v == nil { return nil }
	s := string(*v); return &s
}

func ifaceStrs(ifs []domain.DriveInterface) []string {
	out := make([]string, len(ifs))
	for i, v := range ifs { out[i] = string(v) }
	return out
}
