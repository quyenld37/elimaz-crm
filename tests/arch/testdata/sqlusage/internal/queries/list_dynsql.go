// Package queries: mẫu cho sqlusage_test — gói internal/queries chỉ được nhắc view _v đã công bố.
package queries

const (
	okView      = `SELECT v.sku, f.key FROM catalog.variants_v AS v JOIN platform.file_variants_v AS f ON true`
	badPlatform = `SELECT key FROM platform.file_variants`
	badTable    = `SELECT name FROM catalog.products`
)
