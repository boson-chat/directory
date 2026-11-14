-- Every change to a network (or its deletion) takes the next value of
-- network_seq. The federation feed (GET /v1/feed?since=) pages on it.
CREATE SEQUENCE network_seq;

CREATE TABLE networks (
    id                   text PRIMARY KEY,              -- net_<ULID>
    -- Lowercased, IDNA (punycode), no trailing dot, never an IP address.
    hostname             text NOT NULL,
    port                 integer NOT NULL CHECK (port BETWEEN 1 AND 65535),
    tls                  boolean NOT NULL DEFAULT true,
    name                 text NOT NULL,
    description          text NOT NULL DEFAULT '',
    tags                 text[] NOT NULL DEFAULT '{}',
    languages            text[] NOT NULL DEFAULT '{}',
    is_nsfw              boolean NOT NULL DEFAULT false,
    website_url          text,
    auth_url             text,
    -- Media storage keys, or absolute URLs (imported listings).
    icon_key             text,
    banner_key           text,

    -- Owner: a boson account OR a stack network key (sha256 hex), never both.
    -- Peer-imported rows have neither.
    owner_auth_user_id   text,
    management_key_hash  text,

    -- 'local' or 'peer:<peer id>'; origin_id is the listing's id at its origin.
    origin               text NOT NULL DEFAULT 'local',
    origin_id            text,

    status               text NOT NULL DEFAULT 'pending'
                         CHECK (status IN ('pending', 'verified', 'lapsed', 'blocked')),
    verification_token   text NOT NULL,
    token_issued_at      timestamptz NOT NULL DEFAULT now(),
    last_checked_at      timestamptz,
    -- Last successful DNS check; lapse is measured from here.
    last_verified_at     timestamptz,

    health_status        text NOT NULL DEFAULT 'unknown'
                         CHECK (health_status IN ('unknown', 'up', 'down')),
    health_checked_at    timestamptz,
    user_count           integer,
    user_count_at        timestamptz,

    -- The backend's servers.id for imported rows, so /servers/{uuid} links keep working.
    legacy_id            uuid UNIQUE,

    seq                  bigint NOT NULL DEFAULT nextval('network_seq'),
    created_at           timestamptz NOT NULL DEFAULT now(),
    updated_at           timestamptz NOT NULL DEFAULT now(),

    search_vector        tsvector GENERATED ALWAYS AS (
        setweight(to_tsvector('simple'::regconfig, coalesce(name, '')), 'A') ||
        setweight(to_tsvector('simple'::regconfig, replace(hostname, '.', ' ')), 'B') ||
        setweight(to_tsvector('simple'::regconfig, coalesce(description, '')), 'C')
    ) STORED,

    CONSTRAINT networks_hostname_key UNIQUE (hostname),
    CONSTRAINT networks_origin_check CHECK (origin = 'local' OR origin LIKE 'peer:_%'),
    CONSTRAINT networks_one_owner CHECK (owner_auth_user_id IS NULL OR management_key_hash IS NULL),
    CONSTRAINT networks_peer_unowned CHECK (origin = 'local' OR (owner_auth_user_id IS NULL AND management_key_hash IS NULL))
);

CREATE UNIQUE INDEX networks_origin_id_key ON networks (origin, origin_id) WHERE origin_id IS NOT NULL;
CREATE UNIQUE INDEX networks_seq_key ON networks (seq);
CREATE INDEX networks_search_idx ON networks USING GIN (search_vector);
CREATE INDEX networks_tags_idx ON networks USING GIN (tags);
CREATE INDEX networks_languages_idx ON networks USING GIN (languages);
CREATE INDEX networks_owner_idx ON networks (owner_auth_user_id) WHERE owner_auth_user_id IS NOT NULL;
CREATE INDEX networks_key_idx ON networks (management_key_hash) WHERE management_key_hash IS NOT NULL;
CREATE INDEX networks_status_checked_idx ON networks (status, last_checked_at NULLS FIRST);
CREATE INDEX networks_health_idx ON networks (health_checked_at NULLS FIRST) WHERE status = 'verified';
-- Keyset orders of GET /v1/networks (verified rows only).
CREATE INDEX networks_sort_users_idx ON networks ((coalesce(user_count, -1)) DESC, created_at DESC, id DESC) WHERE status = 'verified';
CREATE INDEX networks_sort_active_idx ON networks ((coalesce(user_count_at, 'epoch'::timestamptz)) DESC, id DESC) WHERE status = 'verified';
CREATE INDEX networks_sort_newest_idx ON networks (created_at DESC, id DESC) WHERE status = 'verified';
CREATE INDEX networks_sort_name_idx ON networks (lower(name), id) WHERE status = 'verified';

-- Deleted networks, so the feed can tell peers. Written by a trigger.
CREATE TABLE network_tombstones (
    seq        bigint PRIMARY KEY,
    network_id text NOT NULL,
    hostname   text NOT NULL,
    port       integer NOT NULL,
    origin     text NOT NULL,
    origin_id  text,
    deleted_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX network_tombstones_deleted_at_idx ON network_tombstones (deleted_at);

-- seq/updated_at move on real changes only. Probe bookkeeping (check and
-- health timestamps, last_verified_at) changes constantly and isn't part
-- of what peers care about, so it doesn't bump seq.
CREATE FUNCTION networks_bump_seq() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE
    ignored text[] := ARRAY['seq', 'updated_at', 'search_vector', 'last_checked_at',
                            'last_verified_at', 'health_checked_at', 'user_count_at'];
BEGIN
    IF TG_OP = 'INSERT' THEN
        -- Serialise seq assignment with commit order, so a feed reader
        -- that has seen seq N never misses a later commit with seq < N.
        PERFORM pg_advisory_xact_lock(7123001);
        NEW.seq := nextval('network_seq');
        RETURN NEW;
    END IF;
    IF (to_jsonb(NEW) - ignored) IS DISTINCT FROM (to_jsonb(OLD) - ignored) THEN
        PERFORM pg_advisory_xact_lock(7123001);
        NEW.seq := nextval('network_seq');
        NEW.updated_at := now();
    END IF;
    RETURN NEW;
END $$;

CREATE TRIGGER networks_bump_seq BEFORE INSERT OR UPDATE ON networks
    FOR EACH ROW EXECUTE FUNCTION networks_bump_seq();

CREATE FUNCTION networks_tombstone() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    PERFORM pg_advisory_xact_lock(7123001);
    INSERT INTO network_tombstones (seq, network_id, hostname, port, origin, origin_id)
    VALUES (nextval('network_seq'), OLD.id, OLD.hostname, OLD.port, OLD.origin, OLD.origin_id);
    RETURN OLD;
END $$;

CREATE TRIGGER networks_tombstone AFTER DELETE ON networks
    FOR EACH ROW EXECUTE FUNCTION networks_tombstone();

CREATE TABLE peers (
    id            text PRIMARY KEY,                     -- per_<ULID>
    url           text NOT NULL UNIQUE,                 -- base URL, no trailing slash
    name          text NOT NULL DEFAULT '',
    enabled       boolean NOT NULL DEFAULT true,
    -- The built-in main directory (DIRECTORY_MAIN_PEER): recreated on start
    -- unless disabled, never silently deleted.
    builtin       boolean NOT NULL DEFAULT false,
    cursor        bigint NOT NULL DEFAULT 0,            -- last applied feed seq
    last_sync_at  timestamptz,
    last_error    text,
    listing_count integer NOT NULL DEFAULT 0,
    created_at    timestamptz NOT NULL DEFAULT now(),
    updated_at    timestamptz NOT NULL DEFAULT now()
);
CREATE UNIQUE INDEX peers_one_builtin ON peers (builtin) WHERE builtin;

-- Hostname blocks: an exact hostname, or a suffix pattern '*.example.com'
-- (matches example.com and every subdomain).
CREATE TABLE blocks (
    id         text PRIMARY KEY,                        -- blk_<ULID>
    pattern    text NOT NULL UNIQUE,
    reason     text NOT NULL DEFAULT '',
    created_by text NOT NULL DEFAULT '',
    created_at timestamptz NOT NULL DEFAULT now()
);

-- Operator and owner actions.
CREATE TABLE audit (
    id         text PRIMARY KEY,                        -- aud_<ULID>
    at         timestamptz NOT NULL DEFAULT now(),
    -- account | network_key | operator | system
    actor_kind text NOT NULL,
    -- auth user id, key hash prefix, or worker name
    actor_id   text NOT NULL DEFAULT '',
    actor_name text NOT NULL DEFAULT '',
    action     text NOT NULL,
    network_id text,
    hostname   text,
    ip         text NOT NULL DEFAULT '',
    meta       jsonb NOT NULL DEFAULT '{}'
);
CREATE INDEX audit_at_idx ON audit (at DESC, id DESC);
CREATE INDEX audit_network_idx ON audit (network_id, at DESC) WHERE network_id IS NOT NULL;
