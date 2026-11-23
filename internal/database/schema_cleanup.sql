-- Explicit cleanup receipts and permanent publication fences. Never age-pruned.
CREATE TABLE IF NOT EXISTS metadata_cleanup_requests (
 id TEXT PRIMARY KEY,
 user_id INTEGER NOT NULL,
 credential_id TEXT NOT NULL,
 request_key TEXT NOT NULL,
 request_hash TEXT NOT NULL,
 operation_id TEXT NOT NULL,
 payload TEXT NOT NULL,
 UNIQUE(user_id,credential_id,request_key)
);
CREATE TABLE IF NOT EXISTS metadata_material_tombstones (
 path TEXT PRIMARY KEY,
 cleanup_id TEXT NOT NULL
);
CREATE TRIGGER IF NOT EXISTS metadata_material_book_insert BEFORE INSERT ON books
WHEN EXISTS(SELECT 1 FROM metadata_material_tombstones WHERE path=NEW.cover_path)
BEGIN SELECT RAISE(ABORT,'retired cover material'); END;
CREATE TRIGGER IF NOT EXISTS metadata_material_book_update BEFORE UPDATE OF cover_path ON books
WHEN EXISTS(SELECT 1 FROM metadata_material_tombstones WHERE path=NEW.cover_path)
BEGIN SELECT RAISE(ABORT,'retired cover material'); END;
CREATE TRIGGER IF NOT EXISTS metadata_material_reference_insert BEFORE INSERT ON metadata_cover_materials
WHEN EXISTS(SELECT 1 FROM metadata_material_tombstones WHERE path=NEW.path)
BEGIN SELECT RAISE(ABORT,'retired cover material'); END;
CREATE TRIGGER IF NOT EXISTS metadata_material_reference_update BEFORE UPDATE OF path ON metadata_cover_materials
WHEN EXISTS(SELECT 1 FROM metadata_material_tombstones WHERE path=NEW.path)
BEGIN SELECT RAISE(ABORT,'retired cover material'); END;
CREATE TRIGGER IF NOT EXISTS metadata_material_stats_insert BEFORE INSERT ON series_stats
WHEN EXISTS(SELECT 1 FROM metadata_material_tombstones WHERE path=NEW.cover_path)
BEGIN SELECT RAISE(ABORT,'retired cover material'); END;
CREATE TRIGGER IF NOT EXISTS metadata_material_stats_update BEFORE UPDATE OF cover_path ON series_stats
WHEN EXISTS(SELECT 1 FROM metadata_material_tombstones WHERE path=NEW.cover_path)
BEGIN SELECT RAISE(ABORT,'retired cover material'); END;
