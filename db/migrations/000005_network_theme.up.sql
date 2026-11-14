-- theme: the listing's boson-theme/1 document (colours, panes, logo, sign-in
-- background), validated by the service; NULL when the listing has none.
-- Image fields hold this directory's media keys (uploads) or absolute URLs
-- (imports, hot-linked). It is part of what peers see, so it bumps seq.
ALTER TABLE networks ADD COLUMN theme jsonb;
