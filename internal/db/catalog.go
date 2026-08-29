package db

import (
	"context"

	"github.com/Balrog57/DiskCount/internal/domain"
	"github.com/jackc/pgx/v5"
)

// ProductCatalog holds canonical technical specs keyed by EAN/SKU identity.
type ProductCatalog struct {
	CanonicalKey, SpecSource string
	EAN, SKU, Brand, Model   *string
	MediaType, DriveCategory *string
	RecordingMethod          *string
	FormFactor, Technology   *string
	ImageURL                 *string
	CapacityTB               float64
	Interfaces               []string
}

func specPriority(src string) int {
	switch src {
	case "keepa":
		return 3
	case "jsonld":
		return 2
	case "heuristic":
		return 1
	default:
		return 0
	}
}

const catalogSelectSQL = `
SELECT canonical_key, ean, sku, brand, model, capacity_tb, media_type, drive_category, recording_method,
       form_factor, technology, interfaces, image_url, spec_source
FROM product_catalog`

func scanCatalog(c *ProductCatalog, row pgx.Row) error {
	return row.Scan(
		&c.CanonicalKey, &c.EAN, &c.SKU, &c.Brand, &c.Model, &c.CapacityTB,
		&c.MediaType, &c.DriveCategory, &c.RecordingMethod, &c.FormFactor, &c.Technology,
		jsonScan(&c.Interfaces), &c.ImageURL, &c.SpecSource)
}

func (db *DB) GetCatalogEntry(ctx context.Context, canonicalKey string) (*ProductCatalog, error) {
	if canonicalKey == "" {
		return nil, nil
	}
	c := &ProductCatalog{}
	err := scanCatalog(c, db.Pool.QueryRow(ctx, catalogSelectSQL+` WHERE canonical_key=$1`, canonicalKey))
	if err == pgx.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return c, nil
}

// CatalogMap returns catalog entries for every canonical key in the input
// slice in a single round-trip. Keys with no entry are absent from the map.
func (db *DB) CatalogMap(ctx context.Context, canonicalKeys []string) (map[string]*ProductCatalog, error) {
	if len(canonicalKeys) == 0 {
		return map[string]*ProductCatalog{}, nil
	}
	rows, err := db.Pool.Query(ctx, catalogSelectSQL+` WHERE canonical_key = ANY($1)`, canonicalKeys)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make(map[string]*ProductCatalog, len(canonicalKeys))
	for rows.Next() {
		c := &ProductCatalog{}
		if err := scanCatalog(c, rows); err != nil {
			return nil, err
		}
		out[c.CanonicalKey] = c
	}
	return out, rows.Err()
}

// EnrichDealFromCatalog fills missing technical fields from the canonical catalog.
func (db *DB) EnrichDealFromCatalog(ctx context.Context, deal *domain.Deal) {
	key := deal.CanonicalProductKey()
	if key == "" {
		return
	}
	cat, err := db.GetCatalogEntry(ctx, key)
	if err != nil || cat == nil {
		return
	}
	ApplyCatalogToDeal(cat, deal)
}

// ApplyCatalogToDeal merges a pre-fetched catalog entry into a deal.
func ApplyCatalogToDeal(cat *ProductCatalog, deal *domain.Deal) {
	applyCatalogToDeal(cat, deal)
}

func applyCatalogToDeal(cat *ProductCatalog, deal *domain.Deal) {
	if deal.Brand == nil && cat.Brand != nil {
		deal.Brand = cat.Brand
	}
	if deal.Model == nil && cat.Model != nil {
		deal.Model = cat.Model
	}
	if deal.EAN == nil && cat.EAN != nil {
		deal.EAN = cat.EAN
	}
	if deal.SKU == nil && cat.SKU != nil {
		deal.SKU = cat.SKU
	}
	if deal.MediaType == nil && cat.MediaType != nil {
		mt := domain.MediaType(*cat.MediaType)
		deal.MediaType = &mt
	}
	if deal.DriveCategory == nil && cat.DriveCategory != nil {
		dc := domain.DriveCategory(*cat.DriveCategory)
		deal.DriveCategory = &dc
	}
	if deal.RecordingMethod == nil && cat.RecordingMethod != nil {
		rm := domain.RecordingMethod(*cat.RecordingMethod)
		deal.RecordingMethod = &rm
	}
	if deal.FormFactor == nil && cat.FormFactor != nil {
		deal.FormFactor = cat.FormFactor
	}
	if deal.Technology == nil && cat.Technology != nil {
		deal.Technology = cat.Technology
	}
	if len(deal.Interfaces) == 0 && len(cat.Interfaces) > 0 {
		deal.Interfaces = ifaceFromStrs(cat.Interfaces)
	}
	if deal.ImageURL == nil && cat.ImageURL != nil {
		deal.ImageURL = cat.ImageURL
	}
	if cat.CapacityTB > 0 && deal.CapacityTB <= 0 {
		deal.CapacityTB = cat.CapacityTB
	}
}

func ifaceFromStrs(ss []string) []domain.DriveInterface {
	out := make([]domain.DriveInterface, 0, len(ss))
	for _, s := range ss {
		out = append(out, domain.DriveInterface(s))
	}
	return out
}

// UpsertCatalogEntry merges observed technical specs into the canonical catalog.
// When prefetched is true, existing is the caller's batch-fetched snapshot for
// this key (nil when the key has no row yet) and GetCatalogEntry is skipped.
//
// ⚡ Bolt optimization: the scanner already loads catalogMap once per scan;
// passing prefetched=true reuses that snapshot and drops one SELECT per
// accepted deal (~200 round-trips on a typical diskprices scan).
func (db *DB) UpsertCatalogEntry(ctx context.Context, deal domain.Deal, existing *ProductCatalog, prefetched bool) error {
	key := deal.CanonicalProductKey()
	if key == "" {
		return nil
	}
	if !prefetched {
		var err error
		existing, err = db.GetCatalogEntry(ctx, key)
		if err != nil {
			return err
		}
	}
	entry := catalogFromDeal(deal)
	if existing != nil && specPriority(entry.SpecSource) < specPriority(existing.SpecSource) {
		entry = mergeCatalogPreferExisting(*existing, entry)
	}
	ifaces := ifaceStrs(deal.Interfaces)
	_, err := db.Pool.Exec(ctx, `
INSERT INTO product_catalog(
  canonical_key, ean, sku, brand, model, capacity_tb, media_type, drive_category, recording_method,
  form_factor, technology, interfaces, image_url, spec_source, updated_at
) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,NOW())
ON CONFLICT (canonical_key) DO UPDATE SET
  ean=COALESCE(EXCLUDED.ean, product_catalog.ean),
  sku=CASE WHEN EXCLUDED.sku IS NOT NULL AND (product_catalog.sku IS NULL OR product_catalog.sku = '') THEN EXCLUDED.sku ELSE product_catalog.sku END,
  brand=COALESCE(EXCLUDED.brand, product_catalog.brand),
  model=COALESCE(EXCLUDED.model, product_catalog.model),
  capacity_tb=CASE WHEN EXCLUDED.capacity_tb > 0 THEN EXCLUDED.capacity_tb ELSE product_catalog.capacity_tb END,
  media_type=COALESCE(EXCLUDED.media_type, product_catalog.media_type),
  drive_category=COALESCE(EXCLUDED.drive_category, product_catalog.drive_category),
  recording_method=COALESCE(EXCLUDED.recording_method, product_catalog.recording_method),
  form_factor=COALESCE(EXCLUDED.form_factor, product_catalog.form_factor),
  technology=COALESCE(EXCLUDED.technology, product_catalog.technology),
  interfaces=CASE WHEN jsonb_array_length(EXCLUDED.interfaces) > 0 THEN EXCLUDED.interfaces ELSE product_catalog.interfaces END,
  image_url=COALESCE(EXCLUDED.image_url, product_catalog.image_url),
  spec_source=CASE WHEN EXCLUDED.spec_source <> '' THEN EXCLUDED.spec_source ELSE product_catalog.spec_source END,
  updated_at=NOW()`,
		key, entry.EAN, entry.SKU, entry.Brand, entry.Model, entry.CapacityTB,
		entry.MediaType, entry.DriveCategory, entry.RecordingMethod,
		entry.FormFactor, entry.Technology, ja(ifaces), entry.ImageURL, entry.SpecSource)
	return err
}

func catalogFromDeal(deal domain.Deal) ProductCatalog {
	return ProductCatalog{
		EAN:              deal.EAN,
		SKU:              deal.SKU,
		Brand:            deal.Brand,
		Model:            deal.Model,
		CapacityTB:       deal.CapacityTB,
		MediaType:        ptrStr(deal.MediaType),
		DriveCategory:    ptrStr(deal.DriveCategory),
		RecordingMethod:  ptrStr(deal.RecordingMethod),
		FormFactor:       deal.FormFactor,
		Technology:       deal.Technology,
		Interfaces:       ifaceStrs(deal.Interfaces),
		ImageURL:         deal.ImageURL,
		SpecSource:       nilIfEmptyStr(deal.ClassificationSource),
	}
}

func mergeCatalogPreferExisting(old, new ProductCatalog) ProductCatalog {
	out := new
	if old.EAN != nil {
		out.EAN = old.EAN
	}
	if old.SKU != nil {
		out.SKU = old.SKU
	}
	if old.Brand != nil {
		out.Brand = old.Brand
	}
	if old.Model != nil {
		out.Model = old.Model
	}
	if old.MediaType != nil {
		out.MediaType = old.MediaType
	}
	if old.DriveCategory != nil {
		out.DriveCategory = old.DriveCategory
	}
	if old.RecordingMethod != nil {
		out.RecordingMethod = old.RecordingMethod
	}
	if old.FormFactor != nil {
		out.FormFactor = old.FormFactor
	}
	if old.Technology != nil {
		out.Technology = old.Technology
	}
	if len(old.Interfaces) > 0 {
		out.Interfaces = old.Interfaces
	}
	if old.ImageURL != nil {
		out.ImageURL = old.ImageURL
	}
	if old.CapacityTB > 0 {
		out.CapacityTB = old.CapacityTB
	}
	out.SpecSource = old.SpecSource
	return out
}

func nilIfEmptyStr(s string) string {
	if s == "" {
		return "heuristic"
	}
	return s
}
