-- Series identification state, read and written only by internal/application/identification.
-- Every row belongs to one series and goes with it (FK cascade). Library relocation keeps series
-- identity, so the rows survive it. Times are Unix milliseconds from the identification clock.
-- Keep this file ASCII-only, and keep semicolons out of comments: Migrate splits statements on them.

-- One conclusion per series and source. confirmer is 'owner' (a person) or 'agent'
-- (user_id plus credential_id). proposal_id names the applied proposal a derived row came from.
CREATE TABLE IF NOT EXISTS identification_conclusions (
    series_id INTEGER NOT NULL REFERENCES series(id) ON DELETE CASCADE,
    source TEXT NOT NULL,
    entry_id TEXT NOT NULL,
    entry_url TEXT NOT NULL DEFAULT '',
    title TEXT NOT NULL DEFAULT '',
    original_title TEXT NOT NULL DEFAULT '',
    aliases TEXT NOT NULL DEFAULT '[]',
    confirmer TEXT NOT NULL CHECK (confirmer IN ('owner', 'agent')),
    user_id INTEGER NOT NULL DEFAULT 0,
    credential_id TEXT NOT NULL DEFAULT '',
    confirmed_at_ms INTEGER NOT NULL,
    reason TEXT NOT NULL DEFAULT '',
    proposal_id INTEGER NOT NULL DEFAULT 0,
    PRIMARY KEY (series_id, source)
);

-- Source entries a person ruled out for the series. Only human decisions write here.
CREATE TABLE IF NOT EXISTS identification_exclusions (
    series_id INTEGER NOT NULL REFERENCES series(id) ON DELETE CASCADE,
    source TEXT NOT NULL,
    entry_id TEXT NOT NULL,
    entry_url TEXT NOT NULL DEFAULT '',
    title TEXT NOT NULL DEFAULT '',
    user_id INTEGER NOT NULL DEFAULT 0,
    excluded_at_ms INTEGER NOT NULL,
    proposal_id INTEGER NOT NULL DEFAULT 0,
    PRIMARY KEY (series_id, source, entry_id)
);

-- Escalations and their outcome. class decides what an open row means for the status:
-- 'identification' waits for the owner, 'recheck' questions an identification, 'note' never
-- changes the status. resolved_at_ms NULL means open.
CREATE TABLE IF NOT EXISTS identification_escalations (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    series_id INTEGER NOT NULL REFERENCES series(id) ON DELETE CASCADE,
    category TEXT NOT NULL,
    class TEXT NOT NULL CHECK (class IN ('identification', 'recheck', 'note')),
    reason TEXT NOT NULL DEFAULT '',
    raised_by TEXT NOT NULL CHECK (raised_by IN ('owner', 'agent', 'system')),
    user_id INTEGER NOT NULL DEFAULT 0,
    credential_id TEXT NOT NULL DEFAULT '',
    raised_at_ms INTEGER NOT NULL,
    resolved_at_ms INTEGER,
    resolved_by INTEGER NOT NULL DEFAULT 0,
    resolution TEXT NOT NULL DEFAULT ''
);
CREATE INDEX IF NOT EXISTS idx_identification_escalations_open
    ON identification_escalations(series_id, class) WHERE resolved_at_ms IS NULL;

-- Not-found conclusion. searched_sources is the JSON array of source keys the verdict covered.
CREATE TABLE IF NOT EXISTS identification_not_found (
    series_id INTEGER PRIMARY KEY REFERENCES series(id) ON DELETE CASCADE,
    searched_sources TEXT NOT NULL DEFAULT '[]',
    confirmer TEXT NOT NULL CHECK (confirmer IN ('owner', 'agent')),
    user_id INTEGER NOT NULL DEFAULT 0,
    credential_id TEXT NOT NULL DEFAULT '',
    concluded_at_ms INTEGER NOT NULL,
    reason TEXT NOT NULL DEFAULT ''
);

-- Owner-reviewed series. Withdrawing deletes the row.
CREATE TABLE IF NOT EXISTS identification_owner_reviews (
    series_id INTEGER PRIMARY KEY REFERENCES series(id) ON DELETE CASCADE,
    user_id INTEGER NOT NULL,
    reviewed_at_ms INTEGER NOT NULL
);

-- Completion marker of the one-time derivation from proposal history.
CREATE TABLE IF NOT EXISTS identification_history_derivation (
    id INTEGER PRIMARY KEY CHECK (id = 1),
    completed_at_ms INTEGER NOT NULL
);

-- The single definition of the identification status. Recreated on every start so a changed
-- rule reaches existing databases. Precedence runs top to bottom.
DROP VIEW IF EXISTS series_identification_status;
CREATE VIEW series_identification_status AS
SELECT s.id AS series_id,
       s.library_id AS library_id,
       CASE
           WHEN EXISTS (SELECT 1 FROM identification_owner_reviews o WHERE o.series_id = s.id)
               THEN 'owner_reviewed'
           WHEN EXISTS (SELECT 1 FROM identification_escalations e
                        WHERE e.series_id = s.id AND e.class = 'recheck' AND e.resolved_at_ms IS NULL)
               THEN 'needs_recheck'
           WHEN EXISTS (SELECT 1 FROM identification_escalations e
                        WHERE e.series_id = s.id AND e.class = 'identification' AND e.resolved_at_ms IS NULL)
               THEN 'awaiting_owner'
           WHEN EXISTS (SELECT 1 FROM identification_conclusions c WHERE c.series_id = s.id)
               THEN 'identified'
           WHEN EXISTS (SELECT 1 FROM identification_not_found n WHERE n.series_id = s.id)
               THEN 'not_found'
           ELSE 'unprocessed'
       END AS status
FROM series s
