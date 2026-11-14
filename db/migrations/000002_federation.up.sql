-- Federation bookkeeping on peers.
--   last_full_sync_at: when the peer's whole feed (since=0) was last walked.
--     A full walk also removes imported listings the peer no longer exports
--     (covers pruned tombstones and conflicts that resolved at the origin).
--   api: the peer's API base from its /.well-known/boson-directory.json.
ALTER TABLE peers
    ADD COLUMN last_full_sync_at timestamptz,
    ADD COLUMN api text NOT NULL DEFAULT '';
