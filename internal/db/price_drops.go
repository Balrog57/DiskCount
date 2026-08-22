package db

import (
	"context"
	"time"
)

// PriceDrop compares the current observation with the immediately preceding
// observation for the same product.
type PriceDrop struct {
	CurrentPrice
	PreviousPricePerTB float64
	PreviousObservedAt time.Time
	DropPct            float64
}

// PriceDrops returns recent, meaningful price decreases, ordered by the
// largest percentage first.
func (db *DB) PriceDrops(ctx context.Context, days int, minDropPct float64, limit int) ([]PriceDrop, error) {
	if days <= 0 {
		days = 30
	}
	if minDropPct < 0 {
		minDropPct = 0
	}
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	rows, err := db.Pool.Query(ctx, `
WITH current AS (
	SELECT DISTINCT ON (product_id) id,product_id,source,observed_at,price_eur,price_per_tb
	FROM price_observations WHERE price_per_tb > 0 AND quality_score >= 70
	ORDER BY product_id,observed_at DESC,id DESC
), compared AS (
	SELECT c.*,previous.price_per_tb AS previous_price_per_tb,previous.observed_at AS previous_observed_at,
		((previous.price_per_tb-c.price_per_tb)/previous.price_per_tb*100) AS drop_pct
	FROM current c JOIN LATERAL (
		SELECT price_per_tb,observed_at FROM price_observations
		WHERE product_id=c.product_id AND price_per_tb > 0 AND quality_score >= 70 AND (observed_at,id)<(c.observed_at,c.id)
		ORDER BY observed_at DESC,id DESC LIMIT 1
	) previous ON TRUE
)
SELECT d.product_id, d.source, p.title, p.url, p.condition, p.media_type, p.drive_category,
       p.brand, p.recording_method, p.interfaces, p.capacity_tb, d.price_eur, d.price_per_tb,
       d.observed_at, d.previous_price_per_tb, d.previous_observed_at, d.drop_pct
FROM compared d JOIN products p ON p.id = d.product_id
WHERE p.quality_score >= 50 AND p.availability='available' AND d.observed_at >= NOW() - ($1 * INTERVAL '1 day')
  AND d.price_per_tb < d.previous_price_per_tb AND d.drop_pct >= $2
ORDER BY d.drop_pct DESC, d.observed_at DESC LIMIT $3`, days, minDropPct, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []PriceDrop
	for rows.Next() {
		var d PriceDrop
		if err := rows.Scan(&d.ProductID, &d.Source, &d.Title, &d.URL, &d.Condition, &d.MediaType,
			&d.DriveCategory, &d.Brand, &d.RecordingMethod, jsonScan(&d.Interfaces), &d.CapacityTB,
			&d.PriceEUR, &d.PricePerTB, &d.ObservedAt, &d.PreviousPricePerTB, &d.PreviousObservedAt, &d.DropPct); err != nil {
			return nil, err
		}
		out = append(out, d)
	}
	return out, rows.Err()
}
