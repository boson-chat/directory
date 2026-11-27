DROP INDEX IF EXISTS networks_previous_hostnames_idx;
ALTER TABLE networks DROP COLUMN previous_hostnames;
DROP TABLE network_moves;
