-- config_managed: the peer is listed in the stack config file's
-- directory.peers (BOSON_CONFIG). Such peers are kept enabled by the file
-- and can't be changed through the peers API; when the file drops one it is
-- disabled (not deleted, its state stays) and config_managed is cleared.
ALTER TABLE peers ADD COLUMN config_managed boolean NOT NULL DEFAULT false;
