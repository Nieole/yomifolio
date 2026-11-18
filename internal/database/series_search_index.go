package database

import (
	"context"
	"database/sql"
	"fmt"
)

// seriesSearchGramSQL encodes character bigrams, including the final character.
// Input is already normalized; hex tokens preserve accents and symbols exactly.
func seriesSearchGramSQL(text string) string {
	return `(SELECT group_concat(hex(substr(x.t, x.i, 2)), ' ') FROM
 (WITH RECURSIVE g(t,i) AS (SELECT ` + text + `, 1 UNION ALL SELECT t, i+1 FROM g WHERE i < length(t)) SELECT t,i FROM g) x)`
}

const seriesSearchProjectionDDL = `CREATE TABLE IF NOT EXISTS series_search_text (
 series_id INTEGER PRIMARY KEY,
 library_id INTEGER NOT NULL,
 name TEXT NOT NULL,
 title TEXT NOT NULL
)`

func seriesSearchTriggerStatements(replace bool) []string {
	statements := []string{seriesSearchProjectionDDL}
	for _, kind := range []string{"search", "gram"} {
		for _, event := range []struct{ suffix, clause string }{
			{"ai", "AFTER INSERT"}, {"ad", "AFTER DELETE"},
			{"au", "AFTER UPDATE OF id, library_id, name, title"},
		} {
			trigger := "trg_series_" + kind + "_fts_" + event.suffix
			if replace {
				statements = append(statements, "DROP TRIGGER IF EXISTS "+trigger)
			}
			table := "series_" + kind + "_fts"
			body := ""
			if event.suffix != "ai" {
				body += "DELETE FROM " + table + " WHERE rowid=OLD.id;"
				if kind == "search" {
					body += "DELETE FROM series_search_text WHERE series_id=OLD.id;"
				}
			}
			if event.suffix != "ad" {
				name, title := "search_normalize(NEW.name)", "search_normalize(COALESCE(NEW.title, ''))"
				if kind == "search" {
					body += "INSERT INTO series_search_text(series_id,library_id,name,title) VALUES(NEW.id,NEW.library_id," + name + "," + title + ");"
				} else {
					name, title = seriesSearchGramSQL(name), seriesSearchGramSQL(title)
				}
				body += "INSERT INTO " + table + "(rowid,library_id,name,title) VALUES(NEW.id,NEW.library_id," + name + "," + title + ");"
			}
			statements = append(statements, fmt.Sprintf("CREATE TRIGGER IF NOT EXISTS %s %s ON series BEGIN %s END", trigger, event.clause, body))
		}
	}
	return statements
}

// The durable projection and both candidate indexes are replaced in one transaction.
// Trigram candidates still require an exact normalized substring check by readers:
// the SQLite tokenizer may fold diacritics more broadly than search_normalize.
func rebuildSeriesSearchIndexContext(ctx context.Context, db *sql.DB) error {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for _, stmt := range []string{
		`DELETE FROM series_search_fts`,
		`DELETE FROM series_gram_fts`,
		`DELETE FROM series_search_text`,
		`INSERT INTO series_search_text(series_id,library_id,name,title)
   SELECT id,library_id,search_normalize(name),search_normalize(COALESCE(title,'')) FROM series`,
		`INSERT INTO series_search_fts(rowid,library_id,name,title) SELECT series_id,library_id,name,title FROM series_search_text`,
		`INSERT INTO series_gram_fts(rowid,library_id,name,title) SELECT series_id,library_id,` + seriesSearchGramSQL("s.name") + `,` + seriesSearchGramSQL("s.title") + ` FROM series_search_text s`,
	} {
		if _, err := tx.ExecContext(ctx, stmt); err != nil {
			return err
		}
	}
	return tx.Commit()
}
