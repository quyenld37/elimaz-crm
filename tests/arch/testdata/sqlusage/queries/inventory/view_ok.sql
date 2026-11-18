-- inventory được gọi catalog, view có trong docs/views.md: hợp lệ; view của chính mình cũng hợp lệ.
SELECT v.sku FROM catalog.variants_v AS v JOIN inventory.stock_balances AS b ON b.variant_id = v.variant_id;
SELECT f.key FROM platform.file_variants_v AS f;
