package api

// Opt-in acceptance measurements, never a wall-clock unit-test assertion. This
// file also runs unchanged against the archived pre-search implementation via
// Go's read-only -overlay mechanism. See scripts/search-acceptance.ps1.
import (
	"context"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"yomifolio/internal/config"
	"yomifolio/internal/database"
	"yomifolio/internal/parser"
	"yomifolio/internal/scanner"
)

func acceptanceName(id int, book bool) string {
	prefix := "海贼王"
	if id%1000 == 0 {
		prefix = "孤星Atlas"
	}
	kind := "Series"
	if book {
		kind = "Book"
	}
	return fmt.Sprintf("%s%s%06d", prefix, kind, id)
}

func acceptanceCheck(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}

// TestSearchAcceptanceSeed creates the immutable v4 mother database through the
// old migration and normal trigger-maintained writes. Paths are deliberately
// synthetic: this database must NOT be fed to a filesystem scanning task.
func TestSearchAcceptanceSeed(t *testing.T) {
	path := os.Getenv("SEARCH_ACCEPTANCE_SEED")
	if path == "" {
		t.Skip("opt-in frozen 200000 books / 20000 series")
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatal("seed destination must not exist")
	}
	acceptanceCheck(t, database.Migrate(path))
	store, err := database.NewStore(path)
	acceptanceCheck(t, err)
	defer store.Close()
	tx, err := store.DB().Begin()
	acceptanceCheck(t, err)
	defer tx.Rollback()
	_, err = tx.Exec(`INSERT INTO libraries(id,name,path) VALUES(1,'frozen','/search-acceptance-synthetic')`)
	acceptanceCheck(t, err)
	series, err := tx.Prepare(`INSERT INTO series(id,library_id,name,title,path,created_at,updated_at) VALUES(?,1,?,'',?,'2026-01-01 00:00:00','2026-01-01 00:00:00')`)
	acceptanceCheck(t, err)
	defer series.Close()
	books, err := tx.Prepare(`INSERT INTO books(id,series_id,library_id,name,title,path,size,file_modified_at,created_at,updated_at) VALUES(?,?,1,?,'',?,1,'2026-01-01 00:00:00','2026-01-01 00:00:00','2026-01-01 00:00:00')`)
	acceptanceCheck(t, err)
	defer books.Close()
	for id := 1; id <= 20000; id++ {
		_, err = series.Exec(id, acceptanceName(id, false), fmt.Sprintf("/search-acceptance-synthetic/s%d", id))
		acceptanceCheck(t, err)
		for j := 1; j <= 10; j++ {
			bid := (id-1)*10 + j
			_, err = books.Exec(bid, id, acceptanceName(bid, true), fmt.Sprintf("/search-acceptance-synthetic/b%d.cbz", bid))
			acceptanceCheck(t, err)
		}
	}
	acceptanceCheck(t, tx.Commit())
	_, err = store.DB().Exec(`PRAGMA wal_checkpoint(TRUNCATE)`)
	acceptanceCheck(t, err)
}

type acceptanceOracleKey struct {
	tier       byte
	pos, extra uint32
	kind       byte
	id         int64
}

func (a acceptanceOracleKey) less(b acceptanceOracleKey) bool {
	if a.tier != b.tier {
		return a.tier < b.tier
	}
	if a.pos != b.pos {
		return a.pos < b.pos
	}
	if a.extra != b.extra {
		return a.extra < b.extra
	}
	if a.kind != b.kind {
		return a.kind < b.kind
	}
	return a.id < b.id
}
func acceptanceOracleRank(field, query string, kind byte, id int64) (acceptanceOracleKey, bool) {
	f, q := []rune(strings.ToLower(field)), []rune(strings.ToLower(query))
	for pos := 0; pos+len(q) <= len(f); pos++ {
		if string(f[pos:pos+len(q)]) == string(q) {
			tier := byte(1)
			if len(f) == len(q) {
				tier = 0
			}
			return acceptanceOracleKey{tier, uint32(pos), uint32(len(f) - len(q)), kind, id}, true
		}
	}
	return acceptanceOracleKey{}, false
}

// Locate an independently computed key using the documented bounded cursor
// wire contract, then use the NEXT server-issued cursor for the measured page.
// This setup does not fetch or time all intervening pages; the timed request is
// an ordinary authenticated HTTP continuation, including near the last result.
func acceptanceLocate(t *testing.T, first string, k acceptanceOracleKey) string {
	t.Helper()
	b, err := base64.RawURLEncoding.DecodeString(first)
	acceptanceCheck(t, err)
	if len(b) != 93 {
		t.Fatal("unsupported cursor format")
	}
	b[75] = k.tier
	binary.BigEndian.PutUint32(b[76:80], k.pos)
	binary.BigEndian.PutUint32(b[80:84], k.extra)
	b[84] = k.kind
	binary.BigEndian.PutUint64(b[85:93], uint64(k.id))
	return base64.RawURLEncoding.EncodeToString(b)
}

type acceptanceSample struct {
	Query     string  `json:"query"`
	Target    string  `json:"target"`
	Phase     string  `json:"phase"`
	Matches   int     `json:"matches"`
	Durations []int64 `json:"nanoseconds"`
	P50       int64   `json:"p50_ns"`
	P95       int64   `json:"p95_ns"`
}
type acceptanceReport struct {
	Label     string             `json:"label"`
	Go        string             `json:"go"`
	CPUs      int                `json:"cpus"`
	SQLite    string             `json:"sqlite"`
	Settings  map[string]string  `json:"settings"`
	MigrateNS int64              `json:"migrate_ns"`
	RebuildNS int64              `json:"search_rebuild_ns"`
	Samples   []acceptanceSample `json:"samples"`
}

func TestSearchAcceptanceMeasure(t *testing.T) {
	input, output := os.Getenv("SEARCH_ACCEPTANCE_INPUT"), os.Getenv("SEARCH_ACCEPTANCE_OUTPUT")
	if input == "" || output == "" {
		t.Skip("opt-in acceptance; see scripts/search-acceptance.ps1")
	}
	fullWalk := os.Getenv("SEARCH_ACCEPTANCE_FULL_WALK") == "1"
	pageSize := 20
	if fullWalk {
		pageSize = 100
	}
	report := acceptanceReport{Label: os.Getenv("SEARCH_ACCEPTANCE_LABEL"), Go: runtime.Version(), CPUs: runtime.NumCPU(), Settings: map[string]string{}}
	// Copy only a closed/checkpointed mother database; never open it for writing.
	work := t.TempDir()
	path := filepath.Join(work, "catalogue.db")
	src, err := os.Open(input)
	acceptanceCheck(t, err)
	dst, err := os.Create(path)
	acceptanceCheck(t, err)
	_, err = io.Copy(dst, src)
	acceptanceCheck(t, err)
	acceptanceCheck(t, src.Close())
	acceptanceCheck(t, dst.Close())
	start := time.Now()
	acceptanceCheck(t, database.Migrate(path))
	report.MigrateNS = time.Since(start).Nanoseconds()
	store, err := database.NewStore(path)
	acceptanceCheck(t, err)
	defer store.Close()
	acceptanceCheck(t, store.DB().QueryRow(`SELECT sqlite_version()`).Scan(&report.SQLite))
	for _, p := range []string{"page_size", "journal_mode", "synchronous", "cache_size", "mmap_size", "temp_store", "user_version"} {
		var value string
		acceptanceCheck(t, store.DB().QueryRow("PRAGMA "+p).Scan(&value))
		report.Settings[p] = value
	}
	var ns, nb int
	acceptanceCheck(t, store.DB().QueryRow(`SELECT count(*) FROM series`).Scan(&ns))
	acceptanceCheck(t, store.DB().QueryRow(`SELECT count(*) FROM books`).Scan(&nb))
	if ns != 20000 || nb != 200000 {
		t.Fatalf("not frozen scale: %d/%d", ns, nb)
	}
	cfg := &config.Config{}
	cfg.Database.Path = path
	cfg.Cache.Dir = filepath.Join(work, "cache")
	cfg.Scanner.ThumbnailFormat = "webp"
	cfg.Scanner.ArchivePoolSize = 5
	config.NormalizeConfig(cfg)
	acceptanceCheck(t, os.MkdirAll(cfg.Cache.Dir, 0755))
	manager := config.NewManager(cfg)
	parser.InitPool(5)
	defer parser.ResetArchivePool()
	c := newAPIFixture(store, scanner.NewScanner(store, manager), manager, filepath.Join(work, "config.yaml"), controllerCacheSizes{imageBytes: 8192, page: 8, bookPageSource: 8})
	defer c.Close()
	mkTestUser(t, store, "acceptance-admin", database.RoleAdmin)
	router := chi.NewRouter()
	c.SetupRoutes(router)
	server := httptest.NewServer(router)
	defer server.Close()
	resp, err := server.Client().Post(server.URL+"/api/auth/login", "application/json", strings.NewReader(`{"username":"acceptance-admin","password":"password1"}`))
	acceptanceCheck(t, err)
	if resp.StatusCode != 200 {
		t.Fatalf("login: %d", resp.StatusCode)
	}
	cookie := resp.Cookies()[0]
	acceptanceCheck(t, resp.Body.Close())
	request := func(query, target, cursor string) ([]string, bool, string) {
		t.Helper()
		req, err := http.NewRequest("GET", server.URL+"/api/search?q="+url.QueryEscape(query)+"&target="+target+fmt.Sprintf("&limit=%d&cursor=", pageSize)+url.QueryEscape(cursor), nil)
		acceptanceCheck(t, err)
		req.AddCookie(cookie)
		resp, err := server.Client().Do(req)
		acceptanceCheck(t, err)
		defer resp.Body.Close()
		if resp.StatusCode != 200 {
			b, _ := io.ReadAll(resp.Body)
			t.Fatalf("search: %d %s", resp.StatusCode, b)
		}
		var result struct {
			Hits []struct {
				ID string `json:"id"`
			} `json:"hits"`
			More   bool   `json:"has_more"`
			Cursor string `json:"next_cursor"`
		}
		acceptanceCheck(t, json.NewDecoder(resp.Body).Decode(&result))
		ids := make([]string, 0, len(result.Hits))
		for _, h := range result.Hits {
			ids = append(ids, h.ID)
		}
		return ids, result.More, result.Cursor
	}
	// Independent literal substring oracle over generator facts, not either SQL
	// implementation. These queries have identical old/new matching semantics.
	queries := []string{"海", "海贼", "海贼王", "星", "孤星", "孤星Atlas"}
	if report.Label != "baseline" {
		queries = append(queries, "孤—星 Ａｔｌａｓ")
	}
	for _, query := range queries {
		if only := os.Getenv("SEARCH_ACCEPTANCE_QUERY"); only != "" && only != query {
			continue
		}
		targets := []string{"series", "book"}
		if fullWalk {
			targets = append(targets, "all")
		}
		for _, target := range targets {
			if only := os.Getenv("SEARCH_ACCEPTANCE_TARGET"); only != "" && only != target {
				continue
			}
			want := map[string]bool{}
			oracleQuery := query
			if query == "孤—星 Ａｔｌａｓ" {
				oracleQuery = "孤星Atlas"
			}
			keys := map[string]acceptanceOracleKey{}
			for id := 1; id <= 20000; id++ {
				sk, sm := acceptanceOracleRank(acceptanceName(id, false), oracleQuery, 1, int64(id))
				if target == "series" || target == "all" {
					if sm {
						identity := fmt.Sprintf("s_%d", id)
						want[identity] = true
						keys[identity] = sk
					}
					if target == "series" {
						continue
					}
				}
				for j := 1; j <= 10; j++ {
					bid := (id-1)*10 + j
					bk, bm := acceptanceOracleRank(acceptanceName(bid, true), oracleQuery, 0, int64(bid))
					if !bm && sm {
						bk = sk
						bk.tier = 2
						bk.kind = 0
						bk.id = int64(bid)
					}
					if sm || bm {
						identity := fmt.Sprintf("b_%d", bid)
						want[identity] = true
						keys[identity] = bk
					}
				}
			}
			// Verify the COMPLETE matching set before comparing latency, including old
			// results beyond the old HTTP cap. HTTP timing below includes real auth.
			seen := map[string]bool{}
			if target == "series" || target == "all" {
				rows, e := store.SearchGlobalSeries(context.Background(), query, 300000)
				acceptanceCheck(t, e)
				for _, r := range rows {
					seen[fmt.Sprintf("s_%d", r.ID)] = true
				}
			}
			if target == "book" || target == "all" {
				rows, e := store.SearchGlobalBooks(context.Background(), query, 300000)
				acceptanceCheck(t, e)
				for _, r := range rows {
					seen[fmt.Sprintf("b_%d", r.ID)] = true
				}
			}
			if len(seen) != len(want) {
				t.Fatalf("oracle %q/%s count %d != %d", query, target, len(seen), len(want))
			}
			for id := range want {
				if !seen[id] {
					t.Fatalf("oracle missing %s", id)
				}
			}
			ordered := make([]string, 0, len(want))
			for identity := range want {
				ordered = append(ordered, identity)
			}
			sort.Slice(ordered, func(i, j int) bool { return keys[ordered[i]].less(keys[ordered[j]]) })
			measure := func(phase, cursor string) {
				if fullWalk {
					return
				}
				s := acceptanceSample{Query: query, Target: target, Phase: phase, Matches: len(want)}
				for i := 0; i < 110; i++ {
					start := time.Now()
					ids, _, _ := request(query, target, cursor)
					elapsed := time.Since(start).Nanoseconds()
					if len(ids) == 0 {
						t.Fatal("empty matching page")
					}
					for _, id := range ids {
						if !want[id] {
							t.Fatalf("HTTP oracle unexpected %s", id)
						}
					}
					if i >= 10 {
						s.Durations = append(s.Durations, elapsed)
					}
				}
				sorted := append([]int64(nil), s.Durations...)
				sort.Slice(sorted, func(i, j int) bool { return sorted[i] < sorted[j] })
				s.P50 = sorted[49]
				s.P95 = sorted[94]
				report.Samples = append(report.Samples, s)
				b, e := json.MarshalIndent(report, "", "  ")
				acceptanceCheck(t, e)
				acceptanceCheck(t, os.WriteFile(output, b, 0644))
				t.Logf("%s %s %s matches=%d p50=%.3fms p95=%.3fms", query, target, phase, len(want), float64(s.P50)/1e6, float64(s.P95)/1e6)
			}
			measure("first", "")
			if report.Label != "baseline" {
				ids, more, first := request(query, target, "")
				if strings.Join(ids, ",") != strings.Join(ordered[:min(pageSize, len(ordered))], ",") {
					t.Fatalf("HTTP first order mismatch %s/%s", query, target)
				}
				if !fullWalk && more && len(want) > 1000 {
					for _, phase := range []string{"middle", "near_end"} {
						boundary := len(ordered) / 2
						if phase == "near_end" {
							boundary = len(ordered) - 41
						}
						locate := acceptanceLocate(t, first, keys[ordered[boundary]])
						ids, more, cursor := request(query, target, locate)
						if !more || strings.Join(ids, ",") != strings.Join(ordered[boundary+1:boundary+21], ",") {
							t.Fatal("independent deep predecessor oracle mismatch")
						}
						ids, _, _ = request(query, target, cursor)
						if strings.Join(ids, ",") != strings.Join(ordered[boundary+21:min(boundary+41, len(ordered))], ",") {
							t.Fatal("independent deep page oracle mismatch")
						}
						measure(phase, cursor)
					}
				}
			}
			// Sparse scenarios can be completely walked at the real HTTP seam. Save
			// the genuinely server-issued last-page cursor outside the timed region.
			if os.Getenv("SEARCH_ACCEPTANCE_LABEL") != "baseline" && (fullWalk || len(want) <= 1000) {
				cursor, last := "", ""
				walk := map[string]bool{}
				position := 0
				for {
					ids, more, next := request(query, target, cursor)
					for _, id := range ids {
						if walk[id] {
							t.Fatalf("duplicate HTTP result %s", id)
						}
						if position >= len(ordered) || id != ordered[position] {
							t.Fatalf("HTTP full order mismatch at %d: %s", position, id)
						}
						position++
						walk[id] = true
					}
					last = cursor
					if !more {
						break
					}
					if next == "" || next == cursor {
						t.Fatal("cursor did not advance")
					}
					cursor = next
				}
				if len(walk) != len(want) {
					t.Fatalf("HTTP walk %d != %d", len(walk), len(want))
				}
				t.Logf("complete HTTP walk %s/%s count=%d page_size=%d", query, target, position, pageSize)
				if last != "" {
					measure("near_end", last)
				}
			}
		}
	}
	start = time.Now()
	acceptanceCheck(t, store.RebuildSeriesSearchIndex(context.Background()))
	acceptanceCheck(t, store.RebuildBookSearchIndex(context.Background()))
	report.RebuildNS = time.Since(start).Nanoseconds()
	b, err := json.MarshalIndent(report, "", "  ")
	acceptanceCheck(t, err)
	acceptanceCheck(t, os.WriteFile(output, b, 0644))
}
