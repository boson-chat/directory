-- channels: the network's fixed channels (names as written, e.g. #lobby);
-- autojoin_channels: the ones new users are joined to. Validated by the
-- service (at most 50 each, '#' then 1..63 printable ASCII without spaces
-- or commas, de-duplicated ignoring case). Additive: existing rows get '{}'.
-- Both are public and federated, so a change bumps seq.
ALTER TABLE networks ADD COLUMN channels text[] NOT NULL DEFAULT '{}';
ALTER TABLE networks ADD COLUMN autojoin_channels text[] NOT NULL DEFAULT '{}';
