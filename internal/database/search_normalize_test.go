package database

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"
)

// Supplemental connection coverage: held connections force distinct pool entries.
func TestSearchNormalizePooledConnections(t *testing.T) {
	db, err := sql.Open("sqlite", filepath.Join(t.TempDir(), "pool.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	ctx := context.Background()
	var held []*sql.Conn
	defer func() {
		for _, c := range held {
			c.Close()
		}
	}()
	for range 4 {
		c, err := db.Conn(ctx)
		if err != nil {
			t.Fatal(err)
		}
		held = append(held, c)
		for _, tc := range []struct{ in, want string }{
			{" Ａ—Straße\u3000E\u0301！", "astrasseé"},
			{"Ｃ＋＋ ① ﬀ", "c++①ff"},
			{"カ\u3099・Ａ", "ガa"},
			{" --\t\n。", ""},
			{"Σςσ", "σσσ"},
		} {
			var got string
			if err := c.QueryRowContext(ctx, `SELECT search_normalize(?)`, tc.in).Scan(&got); err != nil {
				t.Fatal(err)
			}
			if got != tc.want {
				t.Errorf("normalize(%q) = %q, want %q", tc.in, got, tc.want)
			}
		}
	}
}
