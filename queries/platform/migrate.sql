-- Câu của db.Migrate, chạy bằng crm_owner trên một kết nối riêng.

-- name: AdvisoryLock :exec
SELECT pg_advisory_lock(@key::bigint);

-- name: AdvisoryUnlock :exec
SELECT pg_advisory_unlock(@key::bigint);

-- name: MigrationChecksumsExist :one
SELECT (to_regclass('platform.migration_checksums') IS NOT NULL)::boolean AS present;

-- name: ListMigrationChecksums :many
SELECT version, sha256
FROM platform.migration_checksums
ORDER BY version;

-- name: InsertMigrationChecksum :exec
INSERT INTO platform.migration_checksums (version, sha256)
VALUES (@version, @sha256)
ON CONFLICT (version) DO NOTHING;
