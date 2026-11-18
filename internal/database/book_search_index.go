package database

import (
	"context"
	"database/sql"
	"fmt"
)

const bookSearchProjectionDDL = `CREATE TABLE IF NOT EXISTS book_search_text (
 book_id INTEGER PRIMARY KEY,
 series_id INTEGER NOT NULL,
 library_id INTEGER NOT NULL,
 name TEXT NOT NULL,
 title TEXT NOT NULL
)`

func bookSearchTriggerStatements(replace bool) []string {
	statements := []string{bookSearchProjectionDDL}
	for _, kind := range []string{"search", "gram"} {
		for _, event := range []struct{ suffix, clause string }{
			{"ai", "AFTER INSERT"}, {"ad", "AFTER DELETE"},
			{"au", "AFTER UPDATE OF id, series_id, library_id, name, title"},
		} {
			trigger := "trg_book_" + kind + "_fts_" + event.suffix
			if replace {
				statements = append(statements, "DROP TRIGGER IF EXISTS "+trigger)
			}
			table := "book_" + kind + "_fts"
			body := ""
			if event.suffix != "ai" {
				body += "DELETE FROM " + table + " WHERE rowid=OLD.id;"
				if kind == "search" {
					body += "DELETE FROM book_search_text WHERE book_id=OLD.id;"
				}
			}
			if event.suffix != "ad" {
				name, title := "search_normalize(NEW.name)", "search_normalize(COALESCE(NEW.title, ''))"
				if kind == "search" {
					body += "INSERT INTO book_search_text(book_id,series_id,library_id,name,title) VALUES(NEW.id,NEW.series_id,NEW.library_id," + name + "," + title + ");"
				} else {
					name, title = seriesSearchGramSQL(name), seriesSearchGramSQL(title)
				}
				body += "INSERT INTO " + table + "(rowid,series_id,library_id,name,title) VALUES(NEW.id,NEW.series_id,NEW.library_id," + name + "," + title + ");"
			}
			statements = append(statements, fmt.Sprintf("CREATE TRIGGER IF NOT EXISTS %s %s ON books BEGIN %s END", trigger, event.clause, body))
		}
	}
	return statements
}

// The durable projection and both candidate indexes are replaced in one transaction.
// Trigram candidates still require an exact normalized substring check by readers:
// the SQLite tokenizer may fold diacritics more broadly than search_normalize.
func rebuildBookSearchIndexContext(ctx context.Context, db *sql.DB) error {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for _, stmt := range []string{
		`DELETE FROM book_search_fts`,
		`DELETE FROM book_gram_fts`,
		`DELETE FROM book_search_text`,
		`INSERT INTO book_search_text(book_id,series_id,library_id,name,title)
   SELECT id,series_id,library_id,search_normalize(name),search_normalize(COALESCE(title,'')) FROM books`,
		`INSERT INTO book_search_fts(rowid,series_id,library_id,name,title) SELECT book_id,series_id,library_id,name,title FROM book_search_text`,
		`INSERT INTO book_gram_fts(rowid,series_id,library_id,name,title) SELECT book_id,series_id,library_id,` + seriesSearchGramSQL("s.name") + `,` + seriesSearchGramSQL("s.title") + ` FROM book_search_text s`,
	} {
		if _, err := tx.ExecContext(ctx, stmt); err != nil {
			return err
		}
	}
	return tx.Commit()
}
