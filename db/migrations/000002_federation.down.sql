ALTER TABLE peers
    DROP COLUMN IF EXISTS api,
    DROP COLUMN IF EXISTS last_full_sync_at;
