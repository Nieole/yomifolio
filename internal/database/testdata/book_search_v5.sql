CREATE TRIGGER IF NOT EXISTS trg_book_search_fts_ai AFTER INSERT ON books BEGIN
			INSERT INTO book_search_fts(rowid, series_id, library_id, name, title)
			VALUES (NEW.id, NEW.series_id, NEW.library_id, NEW.name, COALESCE(NEW.title, ''));
		END;
CREATE TRIGGER IF NOT EXISTS trg_book_search_fts_ad AFTER DELETE ON books BEGIN
			DELETE FROM book_search_fts WHERE rowid = OLD.id;
		END;
CREATE TRIGGER IF NOT EXISTS trg_book_search_fts_au AFTER UPDATE OF series_id, library_id, name, title ON books BEGIN
			DELETE FROM book_search_fts WHERE rowid = OLD.id;
			INSERT INTO book_search_fts(rowid, series_id, library_id, name, title)
			VALUES (NEW.id, NEW.series_id, NEW.library_id, NEW.name, COALESCE(NEW.title, ''));
		END;
CREATE TRIGGER IF NOT EXISTS trg_book_gram_fts_ai AFTER INSERT ON books BEGIN
			INSERT INTO book_gram_fts(rowid, series_id, library_id, name, title)
			VALUES (NEW.id, NEW.series_id, NEW.library_id, (SELECT group_concat(hex(substr(x.t, x.i, 2)), ' ') FROM (WITH RECURSIVE g(t,i) AS (SELECT lower(NEW.name), 1 UNION ALL SELECT t, i+1 FROM g WHERE i < length(t)) SELECT t,i FROM g) x), (SELECT group_concat(hex(substr(x.t, x.i, 2)), ' ') FROM (WITH RECURSIVE g(t,i) AS (SELECT lower(COALESCE(NEW.title, '')), 1 UNION ALL SELECT t, i+1 FROM g WHERE i < length(t)) SELECT t,i FROM g) x));
		END;
CREATE TRIGGER IF NOT EXISTS trg_book_gram_fts_ad AFTER DELETE ON books BEGIN
			DELETE FROM book_gram_fts WHERE rowid = OLD.id;
		END;
CREATE TRIGGER IF NOT EXISTS trg_book_gram_fts_au AFTER UPDATE OF series_id, library_id, name, title ON books BEGIN
			DELETE FROM book_gram_fts WHERE rowid = OLD.id;
			INSERT INTO book_gram_fts(rowid, series_id, library_id, name, title)
			VALUES (NEW.id, NEW.series_id, NEW.library_id, (SELECT group_concat(hex(substr(x.t, x.i, 2)), ' ') FROM (WITH RECURSIVE g(t,i) AS (SELECT lower(NEW.name), 1 UNION ALL SELECT t, i+1 FROM g WHERE i < length(t)) SELECT t,i FROM g) x), (SELECT group_concat(hex(substr(x.t, x.i, 2)), ' ') FROM (WITH RECURSIVE g(t,i) AS (SELECT lower(COALESCE(NEW.title, '')), 1 UNION ALL SELECT t, i+1 FROM g WHERE i < length(t)) SELECT t,i FROM g) x));
		END;
