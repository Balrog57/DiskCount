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

func (db *DB) GetCatalogEntry(ctx context.Context, canonicalKey string) (*ProductCatalog, error) {
	if canonicalKey == "" {
		return nil, nil
	}
	c := &ProductCatalog{}
	err := db.Pool.QueryRow(ctx, `
SELECT canonical_key, ean, sku, brand, model, capacity_tb, media_type, drive_category, recording_method,
       form_factor, technology, interfaces, image_url, spec_source
FROM product_catalog WHERE canonical_key=$1`, canonicalKey).Scan(
		&c.CanonicalKey, &c.EAN, &c.SKU, &c.Brand, &c.Model, &c.CapacityTB,
		&c.MediaType, &c.DriveCategory, &c.RecordingMethod, &c.FormFactor, &c.Technology,
		jsonScan(&c.Interfaces), &c.ImageURL, &c.SpecSource)
	if err == pgx.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return c, nil
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
func (db *DB) UpsertCatalogEntry(ctx context.Context, deal domain.Deal) error {
	key := deal.CanonicalProductKey()
	if key == "" {
		return nil
	}
	existing, err := db.GetCatalogEntry(ctx, key)
	if err != nil {
		return err
	}
	entry := catalogFromDeal(deal)
	if existing != nil && specPriority(entry.SpecSource) < specPriority(existing.SpecSource) {
		entry = mergeCatalogPreferExisting(*existing, entry)
	}
	ifaces := ifaceStrs(deal.Interfaces)
	_, err = db.Pool.Exec(ctx, `
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
