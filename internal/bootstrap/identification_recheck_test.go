// These tests guard what a real scan does to an identified series: values its identification
// brought in survive a new book's ComicInfo, and a new book whose ComicInfo names another work
// sends the series to recheck without touching those values. Broken, an ordinary weekly scan
// would silently erase source metadata or hide a wrong identification.

package bootstrap_test

import (
	"archive/zip"
	"bytes"
	"context"
	"database/sql"
	"fmt"
	"image"
	"image/png"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"testing"
	"time"

	"yomifolio/internal/application/delegated"
	"yomifolio/internal/database"
	"yomifolio/internal/metadata"
	"yomifolio/internal/seriesmeta"
	"yomifolio/internal/task"
)

// writeComicArchive writes a one-page cbz; an empty comicInfo leaves ComicInfo.xml out.
func writeComicArchive(t *testing.T, path, comicInfo string) {
	t.Helper()
	var archive, picture bytes.Buffer
	if err := png.Encode(&picture, image.NewRGBA(image.Rect(0, 0, 8, 8))); err != nil {
		t.Fatal(err)
	}
	z := zip.NewWriter(&archive)
	files := map[string][]byte{"01.png": picture.Bytes()}
	if comicInfo != "" {
		files["ComicInfo.xml"] = []byte(comicInfo)
	}
	for name, data := range files {
		w, err := z.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := w.Write(data); err != nil {
			t.Fatal(err)
		}
	}
	if err := z.Close(); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, archive.Bytes(), 0o600); err != nil {
		t.Fatal(err)
	}
}

func sqlText(s string) sql.NullString { return sql.NullString{String: s, Valid: true} }

// identifiedSeries leaves one series "Monster" the way an earlier scan and the old review flow
// did before the upgrade: the directory title locked, its first book on disk with firstComicInfo,
// and an applied Bangumi proposal (title 怪物, original MONSTER) whose fields were written with
// their source records. The owner later changed the publisher by hand.
func identifiedSeries(t *testing.T, store database.Store, firstComicInfo string) (database.Library, database.Series) {
	t.Helper()
	ctx := context.Background()
	root := t.TempDir()
	lib, err := store.CreateLibrary(ctx, database.CreateLibraryParams{Name: "Library", Path: root, ScanMode: "none"})
	if err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(root, "Monster")
	first := filepath.Join(dir, "Monster 01.cbz")
	writeComicArchive(t, first, firstComicInfo)
	info, err := os.Stat(first)
	if err != nil {
		t.Fatal(err)
	}
	series, err := store.CreateSeries(ctx, database.CreateSeriesParams{LibraryID: lib.ID, Name: "Monster", Path: dir,
		Title: sqlText("Monster"), LockedFields: sqlText("title"), NameInitial: "M"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.CreateBook(ctx, database.CreateBookParams{LibraryID: lib.ID, SeriesID: series.ID, Name: "Monster 01.cbz", Path: first, Size: info.Size(), FileModifiedAt: info.ModTime(), PageCount: 1}); err != nil {
		t.Fatal(err)
	}
	proposal := historicalProposal(t, store, series.ID, "Bangumi", 4711, "https://bgm.tv/subject/4711", `{"Title":"怪物","OriginalTitle":"MONSTER"}`, "applied")
	err = store.ExecTx(ctx, func(q *database.Queries) error {
		current, err := q.GetSeries(ctx, series.ID)
		if err != nil {
			return err
		}
		if err := seriesmeta.ApplyProposal(ctx, q, current, &metadata.SeriesMetadata{Title: "怪物", Summary: "Source summary", Publisher: "Source publisher", Status: "completed", Rating: 8.6},
			seriesmeta.ProposalInput{ProviderName: "Bangumi", SourceURL: "https://bgm.tv/subject/4711", Confidence: 1, ProposalID: &proposal}); err != nil {
			return err
		}
		current, err = q.GetSeries(ctx, series.ID)
		if err != nil {
			return err
		}
		_, err = q.UpdateSeriesMetadata(ctx, database.UpdateSeriesMetadataParams{ID: current.ID, Title: current.Title, Summary: current.Summary,
			Publisher: sqlText("Owner publisher"), Status: current.Status, Rating: current.Rating, Language: current.Language, LockedFields: current.LockedFields, NameInitial: current.NameInitial})
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	return lib, series
}

// scan starts a library scan the way the web page does and waits for that run to finish.
func (w *identificationWeb) scan(library int64, force bool) task.Run {
	w.t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	before := w.latestScan(ctx)
	path := "/api/libraries/" + strconv.FormatInt(library, 10) + "/scan"
	if force {
		path += "?force=true"
	}
	if rec := w.raw("POST", path, ""); rec.Code != http.StatusOK {
		w.t.Fatalf("scan %d %s", rec.Code, rec.Body)
	}
	started := w.latestScan(ctx)
	if started == before {
		w.t.Fatal("the scan request admitted no run")
	}
	run, err := w.process.Application.Engine.Await(ctx, started)
	if err != nil {
		w.t.Fatal(err)
	}
	return run
}

func (w *identificationWeb) latestScan(ctx context.Context) int64 {
	w.t.Helper()
	runs, err := w.process.Application.Tasks.ListRuns(ctx, task.RunFilter{Types: []task.Type{"scan_library"}, Order: task.OrderIDDesc, Limit: 1})
	if err != nil {
		w.t.Fatal(err)
	}
	if len(runs) == 0 {
		return 0
	}
	return runs[0].ID
}

// series reads the series metadata the series page shows.
func (w *identificationWeb) series(id int64) database.Series {
	w.t.Helper()
	var view struct {
		Series database.Series `json:"series"`
	}
	w.call("GET", "/api/series/"+strconv.FormatInt(id, 10)+"/context", http.StatusOK, &view)
	return view.Series
}

func TestScanKeepsSourceBackedValuesWhenANewBookBringsComicInfo(t *testing.T) {
	store, cfg := fixture(t)
	// The scan re-reads the first book too, in either order; both agree on what they carry.
	lib, series := identifiedSeries(t, store, `<ComicInfo><Series>Monster</Series><Publisher>ComicInfo publisher</Publisher><LanguageISO>ja</LanguageISO></ComicInfo>`)
	web := openIdentificationWeb(t, store, cfg)
	defer web.process.Close()
	web.setupAdmin()

	writeComicArchive(t, filepath.Join(lib.Path, "Monster", "Monster 02.cbz"), `<ComicInfo><Series>Monster</Series><Summary>ComicInfo summary</Summary>`+
		`<Publisher>ComicInfo publisher</Publisher><LanguageISO>ja</LanguageISO><CommunityRating>3.5</CommunityRating></ComicInfo>`)
	if run := web.scan(lib.ID, false); run.Status != task.StatusCompleted {
		t.Fatalf("scan %+v", run)
	}
	got := web.series(series.ID)
	// Values the identification brought in, still as recorded, survive; the publisher the owner
	// changed and the language nobody recorded follow ComicInfo as before.
	if got.Summary.String != "Source summary" || got.Status.String != "completed" || got.Rating.Float64 != 8.6 || got.Title.String != "Monster" {
		t.Fatalf("source-backed values after the scan: %+v", got)
	}
	if got.Publisher.String != "ComicInfo publisher" || got.Language.String != "ja" {
		t.Fatalf("values without a matching source record after the scan: %+v", got)
	}
	if status := web.identification(series.ID).Status; status != "identified" {
		t.Fatalf("a consistent new book changed the status to %s", status)
	}
}

func TestNewBookNamingAnotherWorkSendsSeriesToRecheck(t *testing.T) {
	store, cfg := fixture(t)
	lib, series := identifiedSeries(t, store, `<ComicInfo><Series>Monster</Series></ComicInfo>`)
	web := openIdentificationWeb(t, store, cfg)
	defer web.process.Close()
	web.setupAdmin()
	before := web.identification(series.ID)

	writeComicArchive(t, filepath.Join(lib.Path, "Monster", "Monster 02.cbz"), `<ComicInfo><Series>Pluto</Series><Summary>Pluto summary</Summary></ComicInfo>`)
	if run := web.scan(lib.ID, false); run.Status != task.StatusCompleted {
		t.Fatalf("scan %+v", run)
	}
	after := web.identification(series.ID)
	if after.Status != "needs_recheck" || !reflect.DeepEqual(after.Conclusions, before.Conclusions) || len(after.Conclusions) != 1 {
		t.Fatalf("identification after a contradicting book: %+v (before %+v)", after, before)
	}
	got := web.series(series.ID)
	if got.Summary.String != "Source summary" || got.Status.String != "completed" || got.Rating.Float64 != 8.6 || got.Title.String != "Monster" || got.BookCount != 2 {
		t.Fatalf("series after a contradicting book: %+v", got)
	}

	agent := web.identificationAgent(cfg, 1, []int64{lib.ID}, []string{"read"})
	counts, _ := progress(t, agent, map[string]any{})
	if counts.Series != 1 || counts.Statuses["needs_recheck"] != 1 || counts.Statuses["identified"] != 0 {
		t.Fatalf("progress after a contradicting book: %+v", counts)
	}
}

func TestBooksThatDoNotContradictLeaveTheIdentificationAlone(t *testing.T) {
	store, cfg := fixture(t)
	// The first book already named another work when the series was identified.
	lib, series := identifiedSeries(t, store, `<ComicInfo><Series>Pluto</Series></ComicInfo>`)
	web := openIdentificationWeb(t, store, cfg)
	defer web.process.Close()
	web.setupAdmin()
	dir := filepath.Join(lib.Path, "Monster")
	status := func(step string) {
		t.Helper()
		if got := web.identification(series.ID).Status; got != "identified" {
			t.Fatalf("%s: status %s", step, got)
		}
	}

	writeComicArchive(t, filepath.Join(dir, "Monster 02.cbz"), "")
	writeComicArchive(t, filepath.Join(dir, "Monster 03.cbz"), `<ComicInfo><Summary>No series name</Summary></ComicInfo>`)
	writeComicArchive(t, filepath.Join(dir, "Monster 04.cbz"), `<ComicInfo><Series>Monster 完全版</Series></ComicInfo>`)
	writeComicArchive(t, filepath.Join(dir, "Monster 05.cbz"), `<ComicInfo><Series>怪物</Series></ComicInfo>`)
	if run := web.scan(lib.ID, false); run.Status != task.StatusCompleted {
		t.Fatalf("scan %+v", run)
	}
	status("books without a series name or naming the identified work")

	if run := web.scan(lib.ID, true); run.Status != task.StatusCompleted {
		t.Fatalf("forced scan %+v", run)
	}
	status("a forced scan re-reading the old book")

	review := "/api/identification/series/" + strconv.FormatInt(series.ID, 10) + "/owner-review"
	web.call("PUT", review, http.StatusOK, nil)
	writeComicArchive(t, filepath.Join(dir, "Monster 06.cbz"), `<ComicInfo><Series>Pluto</Series></ComicInfo>`)
	if run := web.scan(lib.ID, false); run.Status != task.StatusCompleted {
		t.Fatalf("scan %+v", run)
	}
	web.call("DELETE", review, http.StatusOK, nil)
	status("a contradicting book added while the owner kept the series")
	if got := web.series(series.ID); got.BookCount != 6 {
		t.Fatalf("books after the scans: %+v", got)
	}
}

func TestFailedRecheckLeavesTheScanCompleted(t *testing.T) {
	store, cfg := fixture(t)
	lib, series := identifiedSeries(t, store, `<ComicInfo><Series>Monster</Series></ComicInfo>`)
	// Fault injection: the check's only write fails.
	if _, err := store.DB().Exec(`CREATE TRIGGER reject_escalation BEFORE INSERT ON identification_escalations BEGIN SELECT RAISE(FAIL, 'rejected'); END`); err != nil {
		t.Fatal(err)
	}
	web := openIdentificationWeb(t, store, cfg)
	defer web.process.Close()
	web.setupAdmin()

	writeComicArchive(t, filepath.Join(lib.Path, "Monster", "Monster 02.cbz"), `<ComicInfo><Series>Pluto</Series></ComicInfo>`)
	if run := web.scan(lib.ID, false); run.Status != task.StatusCompleted {
		t.Fatalf("scan with a failing check %+v", run)
	}
	if got := web.series(series.ID); got.BookCount != 2 {
		t.Fatalf("books after the scan: %+v", got)
	}
	if got := web.identification(series.ID).Status; got != "identified" {
		t.Fatalf("status after a failed check: %s", got)
	}
}

func TestIdentificationKeepsTheEntrysAliasesAndTheRecheckAcceptsThem(t *testing.T) {
	store, cfg := fixture(t)
	unpaced(t, cfg)
	library := newPrefetchLibrary(t, store, "Aliases")
	agentMade := library.series("Monster", "", "01.cbz")
	ownerMade := library.series("Boys", "", "01.cbz")
	fromHistory := library.series("Pluto", "", "01.cbz")
	historicalProposal(t, store, fromHistory, "Bangumi", 4711, "https://bgm.tv/subject/4711", `{"Title":"冥王","OriginalTitle":"PLUTO","Aliases":["Puruto"]}`, "applied")
	sources := &recordingSources{answer: catalogue(map[string]map[string]*metadata.SeriesMetadata{
		"bangumi": {
			"Monster": {Title: "怪物", OriginalTitle: "MONSTER", Aliases: []string{"Kaibutsu", "莫扎特的怪物"}, SourceID: 1, SourceURL: "https://bgm.tv/subject/1"},
			"Boys":    {Title: "二十世纪少年", OriginalTitle: "20世紀少年", Aliases: []string{"Nijusseiki Shonen"}, SourceID: 2, SourceURL: "https://bgm.tv/subject/2"},
		},
	})}
	web := openClaimWeb(t, store, cfg, sources, &handClock{now: time.Date(2026, 3, 1, 9, 0, 0, 0, time.UTC)})
	if _, run := web.prefetch(fmt.Sprintf(`{"library_id":%d}`, library.lib.ID)); run.Status != task.StatusCompleted {
		t.Fatalf("prefetch run %+v", run)
	}
	agent := web.mainLoopAgent(cfg, 1, []int64{library.lib.ID}, []string{delegated.Read, delegated.MetadataWrite})
	agent.identifyAll("identify", map[int64][]string{agentMade: {"bangumi:1"}})
	web.pickEntry(ownerMade, "bangumi", web.manualSearch(ownerMade, "bangumi", "Boys")[0], http.StatusOK)

	// Each conclusion keeps the entry's original title and aliases, however it was reached.
	for series, want := range map[int64][]string{agentMade: {"Kaibutsu", "莫扎特的怪物"}, ownerMade: {"Nijusseiki Shonen"}, fromHistory: {"Puruto"}} {
		if got := web.identification(series).Conclusions; len(got) != 1 || !reflect.DeepEqual(got[0].Aliases, want) {
			t.Fatalf("series %d conclusions %+v, want aliases %q", series, got, want)
		}
	}

	// A new book named by an alias is the identified work; another name still questions it.
	web.addBook(library, "Monster", "02.cbz", "Kaibutsu")
	web.addBook(library, "Boys", "02.cbz", "nijusseiki shonen")
	web.addBook(library, "Pluto", "02.cbz", "PURUTO")
	for _, series := range []int64{agentMade, ownerMade, fromHistory} {
		if got := web.identification(series).Status; got != "identified" {
			t.Fatalf("series %d named by an alias: %s", series, got)
		}
	}
	web.addBook(library, "Monster", "03.cbz", "Astro Boy")
	if got := web.identification(agentMade).Status; got != "needs_recheck" {
		t.Fatalf("series named by another work: %s", got)
	}
}
