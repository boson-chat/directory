-- Runtime settings the operator changes from the admin panel (or the
-- operator API, /v1/admin/settings). Exactly one row. A NULL column means
-- "use the env default" (DIRECTORY_NAME, DIRECTORY_INCLUDE_MAIN, ...), so
-- the env only seeds defaults until an operator overrides a value here.
--   seeded_peers: DIRECTORY_PEERS URLs already seeded once; a peer the
--     operator removes later is not re-added on the next start.
CREATE TABLE directory_settings (
    id                 smallint PRIMARY KEY DEFAULT 1 CHECK (id = 1),
    name               text,
    operator           text,
    contact            text,
    include_main       boolean,
    main_peer_url      text,
    sync_every_seconds integer CHECK (sync_every_seconds IS NULL OR sync_every_seconds > 0),
    seeded_peers       jsonb NOT NULL DEFAULT '[]',
    updated_at         timestamptz NOT NULL DEFAULT now(),
    updated_by         text
);

INSERT INTO directory_settings (id) VALUES (1);
