-- Series identification state, written only by internal/application/identification. The library
-- browse filter also reads the status view.
-- Every row belongs to one series and goes with it (FK cascade). Library relocation keeps series
-- identity, so the rows survive it. Times are Unix milliseconds from the identification clock.
-- Keep this file ASCII-only, and keep semicolons out of comments: Migrate splits statements on them.

-- One conclusion per series and source. confirmer is 'owner' (a person) or 'agent'
-- (user_id plus credential_id). proposal_id names the applied proposal a derived row came from.
-- facts is the validated source snapshot of the entry enrichment fills fields from, '{}' when the
-- conclusion came without one (derived from history).
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
    facts TEXT NOT NULL DEFAULT '{}',
    PRIMARY KEY (series_id, source)
);

-- Source entries a person ruled out for the series. Only human decisions write here, and the
-- owner's own pick of an excluded entry deletes its row.
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

-- The evidence packet of a series, replaced whole by every fetch. packet is the text view served
-- to agents and the owner. source_facts maps each candidate id in it to the validated source
-- snapshot that enrichment fills fields from.
CREATE TABLE IF NOT EXISTS identification_evidence (
    series_id INTEGER PRIMARY KEY REFERENCES series(id) ON DELETE CASCADE,
    fetched_at_ms INTEGER NOT NULL,
    packet TEXT NOT NULL,
    source_facts TEXT NOT NULL DEFAULT '{}'
);

-- The claim lease of a series: one agent session holds it until expires_at_ms. A finishing verdict
-- deletes the row. An expired row stays until another claim replaces it, so its holder can still
-- answer while nobody else has claimed the series.
CREATE TABLE IF NOT EXISTS identification_leases (
    series_id INTEGER PRIMARY KEY REFERENCES series(id) ON DELETE CASCADE,
    lease_id TEXT NOT NULL,
    user_id INTEGER NOT NULL,
    credential_id TEXT NOT NULL,
    claimed_at_ms INTEGER NOT NULL,
    expires_at_ms INTEGER NOT NULL
);

-- The decided result of each verdict, by the credential's request key, so a retried request gets
-- the original result back. digest fingerprints the verdict the key was first used with.
CREATE TABLE IF NOT EXISTS identification_verdicts (
    credential_id TEXT NOT NULL,
    request_key TEXT NOT NULL,
    series_id INTEGER NOT NULL REFERENCES series(id) ON DELETE CASCADE,
    user_id INTEGER NOT NULL,
    digest TEXT NOT NULL,
    result TEXT NOT NULL,
    decided_at_ms INTEGER NOT NULL,
    PRIMARY KEY (credential_id, request_key, series_id)
);
CREATE INDEX IF NOT EXISTS idx_identification_verdicts_series ON identification_verdicts(series_id);

-- Series the incremental prefetch is asked to prepare a packet for: ones a scan created, and
-- ones a claim found with a packet no longer current. Storing a packet ends the request, and a
-- request whose series no longer needs one is dropped by the next incremental run.
CREATE TABLE IF NOT EXISTS identification_prefetch_requests (
    series_id INTEGER PRIMARY KEY REFERENCES series(id) ON DELETE CASCADE,
    requested_at_ms INTEGER NOT NULL
);

-- The catalogue sources the configuration enables and since when. Sources enabled when the table
-- was first filled carry 0. A not-found verdict concluded before a source was enabled, and that did
-- not search it, is reopened for that source.
CREATE TABLE IF NOT EXISTS identification_enabled_sources (
    source TEXT PRIMARY KEY,
    enabled_at_ms INTEGER NOT NULL
);

-- Completion marker of the one-time derivation from proposal history.
CREATE TABLE IF NOT EXISTS identification_history_derivation (
    id INTEGER PRIMARY KEY CHECK (id = 1),
    completed_at_ms INTEGER NOT NULL
);

-- The single definition of the identification status. Recreated on every start so a changed
-- rule reaches existing databases. Precedence runs top to bottom. A not-found verdict a newly
-- enabled source reopens leaves the series unprocessed.
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
           WHEN EXISTS (SELECT 1 FROM identification_not_found n WHERE n.series_id = s.id
                        AND NOT EXISTS (SELECT 1 FROM identification_enabled_sources x
                                        WHERE x.enabled_at_ms > n.concluded_at_ms
                                          AND x.source NOT IN (SELECT value FROM json_each(n.searched_sources))))
               THEN 'not_found'
           ELSE 'unprocessed'
       END AS status
FROM series s
