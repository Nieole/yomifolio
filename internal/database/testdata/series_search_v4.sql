-- Actual version 4 series index triggers.
CREATE TRIGGER IF NOT EXISTS trg_series_search_fts_ai AFTER INSERT ON series BEGIN
			INSERT INTO series_search_fts(rowid, library_id, name, title)
			VALUES (NEW.id, NEW.library_id, NEW.name, COALESCE(NEW.title, ''));
		END;
CREATE TRIGGER IF NOT EXISTS trg_series_search_fts_ad AFTER DELETE ON series BEGIN
			DELETE FROM series_search_fts WHERE rowid = OLD.id;
		END;
CREATE TRIGGER IF NOT EXISTS trg_series_search_fts_au AFTER UPDATE OF library_id, name, title ON series BEGIN
			DELETE FROM series_search_fts WHERE rowid = OLD.id;
			INSERT INTO series_search_fts(rowid, library_id, name, title)
			VALUES (NEW.id, NEW.library_id, NEW.name, COALESCE(NEW.title, ''));
		END;
CREATE TRIGGER IF NOT EXISTS trg_series_gram_fts_ai AFTER INSERT ON series BEGIN
			INSERT INTO series_gram_fts(rowid, library_id, name, title)
			VALUES (NEW.id, NEW.library_id, (SELECT group_concat(hex(substr(x.t, x.i, 2)), ' ') FROM (WITH RECURSIVE g(t,i) AS (SELECT lower(NEW.name), 1 UNION ALL SELECT t, i+1 FROM g WHERE i < length(t)) SELECT t,i FROM g) x), (SELECT group_concat(hex(substr(x.t, x.i, 2)), ' ') FROM (WITH RECURSIVE g(t,i) AS (SELECT lower(COALESCE(NEW.title, '')), 1 UNION ALL SELECT t, i+1 FROM g WHERE i < length(t)) SELECT t,i FROM g) x));
		END;
CREATE TRIGGER IF NOT EXISTS trg_series_gram_fts_ad AFTER DELETE ON series BEGIN
			DELETE FROM series_gram_fts WHERE rowid = OLD.id;
		END;
CREATE TRIGGER IF NOT EXISTS trg_series_gram_fts_au AFTER UPDATE OF library_id, name, title ON series BEGIN
			DELETE FROM series_gram_fts WHERE rowid = OLD.id;
			INSERT INTO series_gram_fts(rowid, library_id, name, title)
			VALUES (NEW.id, NEW.library_id, (SELECT group_concat(hex(substr(x.t, x.i, 2)), ' ') FROM (WITH RECURSIVE g(t,i) AS (SELECT lower(NEW.name), 1 UNION ALL SELECT t, i+1 FROM g WHERE i < length(t)) SELECT t,i FROM g) x), (SELECT group_concat(hex(substr(x.t, x.i, 2)), ' ') FROM (WITH RECURSIVE g(t,i) AS (SELECT lower(COALESCE(NEW.title, '')), 1 UNION ALL SELECT t, i+1 FROM g WHERE i < length(t)) SELECT t,i FROM g) x));
		END;
