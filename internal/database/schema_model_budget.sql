-- Model-budget operations are not metadata change audits.
CREATE TABLE IF NOT EXISTS mcp_model_operations (
 id TEXT PRIMARY KEY,
 user_id INTEGER NOT NULL,
 credential_id TEXT NOT NULL,
 library_id INTEGER NOT NULL,
 series_id INTEGER NOT NULL,
 purpose TEXT NOT NULL CHECK (purpose = 'match_assessment'),
 search_id TEXT NOT NULL,
 allowance INTEGER NOT NULL CHECK (allowance > 0 AND allowance <= 50),
 used INTEGER NOT NULL DEFAULT 0 CHECK (used >= 0 AND used <= allowance),
 run_id INTEGER NOT NULL DEFAULT 0,
 evidence TEXT NOT NULL,
 UNIQUE (credential_id, search_id)
);
CREATE TABLE IF NOT EXISTS mcp_model_calls (
 operation_id TEXT NOT NULL REFERENCES mcp_model_operations(id),
 item INTEGER NOT NULL,
 outcome TEXT NOT NULL DEFAULT 'unknown',
 PRIMARY KEY (operation_id, item)
);
