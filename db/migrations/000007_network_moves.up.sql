-- Moving a listing to a new hostname (POST /v1/networks/{id}/move).
--
-- network_moves: one row per move request. A request is 'pending' until the
-- new hostname's _boson TXT record carries the move's token (strict 3/3,
-- within the token TTL), then 'completed'; or 'cancelled' by the owner, or
-- 'expired' by the verify worker once the token is older than the TTL.
-- At most one pending request per listing. The row is history, so it's kept
-- after the move (and dropped with the listing). It lives outside networks
-- so that a pending move (an unverified hostname) never bumps seq and never
-- reaches the feed.
CREATE TABLE network_moves (
    id              text PRIMARY KEY,                        -- mov_<ULID>
    network_id      text NOT NULL REFERENCES networks (id) ON DELETE CASCADE,
    from_hostname   text NOT NULL,
    from_port       integer NOT NULL,
    from_tls        boolean NOT NULL,
    to_hostname     text NOT NULL,
    to_port         integer NOT NULL CHECK (to_port BETWEEN 1 AND 65535),
    to_tls          boolean NOT NULL,
    token           text NOT NULL,
    status          text NOT NULL DEFAULT 'pending'
                    CHECK (status IN ('pending', 'completed', 'cancelled', 'expired')),
    requested_at    timestamptz NOT NULL DEFAULT now(),
    token_issued_at timestamptz NOT NULL DEFAULT now(),
    last_checked_at timestamptz,
    -- When the request stopped being pending (completed, cancelled, expired).
    ended_at        timestamptz,
    -- Who asked: account | network_key | operator (audit actor kinds).
    actor_kind      text NOT NULL DEFAULT '',
    actor_id        text NOT NULL DEFAULT ''
);
CREATE UNIQUE INDEX network_moves_one_pending ON network_moves (network_id) WHERE status = 'pending';
CREATE INDEX network_moves_network_idx ON network_moves (network_id, requested_at DESC);
CREATE INDEX network_moves_due_idx ON network_moves (last_checked_at NULLS FIRST) WHERE status = 'pending';

-- previous_hostnames: the hostnames a listing was moved away from, most
-- recent first (at most 10). Public and federated (bumps seq), so lookups
-- and peers can follow a move. Additive: existing rows get '{}'.
ALTER TABLE networks ADD COLUMN previous_hostnames text[] NOT NULL DEFAULT '{}';
CREATE INDEX networks_previous_hostnames_idx ON networks USING GIN (previous_hostnames);
