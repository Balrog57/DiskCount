package db

import (
	"context"
	"time"
)

// MarketIndexPoint is the daily median price per TB for one capacity band.
type MarketIndexPoint struct {
	Day       time.Time
	Band      string
	MedianEUR float64
	Samples   int64
}

// MarketIndex computes the daily market index directly from observations.
// It intentionally uses the observation history as the source of truth;
// there is no cache or materialized table to keep in sync.
func (db *DB) MarketIndex(ctx context.Context, days int) ([]MarketIndexPoint, error) {
	if days <= 0 || days > 365 {
		days = 30
	}
	rows, err := db.Pool.Query(ctx, `
WITH qualified AS (
	SELECT o.id,o.product_id,o.observed_at,o.price_per_tb,p.capacity_tb,
		date_trunc('day',o.observed_at AT TIME ZONE 'UTC') AT TIME ZONE 'UTC' AS day
	FROM price_observations o JOIN products p ON p.id=o.product_id
	WHERE o.observed_at >= NOW()-($1*INTERVAL '1 day') AND o.price_per_tb > 0
		AND o.quality_score >= 70 AND p.quality_score >= 50 AND p.capacity_tb > 0
), daily_product AS (
	SELECT DISTINCT ON (product_id,day) id,product_id,day,capacity_tb,price_per_tb
	FROM qualified ORDER BY product_id,day,observed_at DESC,id DESC
)
SELECT day,
       CASE
         WHEN capacity_tb < 4 THEN '<4 To'
         WHEN capacity_tb < 8 THEN '4–8 To'
         WHEN capacity_tb < 12 THEN '8–12 To'
         WHEN capacity_tb < 16 THEN '12–16 To'
         WHEN capacity_tb < 20 THEN '16–20 To'
         WHEN capacity_tb < 24 THEN '20–24 To'
         WHEN capacity_tb < 30 THEN '24–30 To'
         ELSE '30+ To'
       END AS band,
       percentile_cont(0.5) WITHIN GROUP (ORDER BY price_per_tb)::float8 AS median_eur,
       count(*)
FROM daily_product GROUP BY day,band
ORDER BY day ASC,min(capacity_tb) ASC`, days)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []MarketIndexPoint
	for rows.Next() {
		var p MarketIndexPoint
		if err := rows.Scan(&p.Day, &p.Band, &p.MedianEUR, &p.Samples); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}
