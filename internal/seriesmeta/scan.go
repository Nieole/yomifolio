package seriesmeta

import (
	"database/sql"
	"strconv"
	"strings"

	"yomifolio/internal/database"
	"yomifolio/internal/parser"
)

// ScanMetadata contains only metadata columns for the scanner's identity upsert.
// It cannot supply library ownership, paths, counts, books or covers.
type ScanMetadata struct {
	Title        sql.NullString
	Summary      sql.NullString
	Publisher    sql.NullString
	Status       sql.NullString
	Rating       sql.NullFloat64
	Language     sql.NullString
	LockedFields sql.NullString
	NameInitial  string
}

// PrepareScanInitial supplies creation defaults without creating a series or transaction.
func PrepareScanInitial(directoryName string, info *parser.ComicInfo) ScanMetadata {
	result := scanArchiveMetadata(info)
	result.Title = sql.NullString{String: directoryName, Valid: true}
	result.LockedFields = sql.NullString{String: "title", Valid: true}
	result.NameInitial = database.SeriesInitial(directoryName, directoryName)
	return result
}

// PrepareScanUpdate prepares the scalars of an existing series for a new or changed book with
// ComicInfo. current and records are the series row and its source records as read in the scan's
// write transaction. The title stays, falling back to the directory name only when empty and
// unlocked. Every other scalar follows ComicInfo, cleared when ComicInfo lacks it (status always,
// ComicInfo carries none), except locked fields and fields SourceBacked reports: those keep their
// current value without gaining a lock, so later corrections stay possible.
func PrepareScanUpdate(current database.Series, records []database.SeriesMetadataProvenance, directoryName string, info *parser.ComicInfo) ScanMetadata {
	result := scanArchiveMetadata(info)
	kept := SourceBacked(current, records)
	for field := range lockedFields(current.LockedFields) {
		kept[field] = true
	}
	result.Title = current.Title
	if !kept["title"] && strings.TrimSpace(result.Title.String) == "" {
		result.Title = sql.NullString{String: directoryName, Valid: true}
	}
	for _, field := range []struct {
		name     string
		target   *sql.NullString
		previous sql.NullString
	}{
		{"summary", &result.Summary, current.Summary},
		{"publisher", &result.Publisher, current.Publisher},
		{"status", &result.Status, current.Status},
		{"language", &result.Language, current.Language},
	} {
		if kept[field.name] {
			*field.target = field.previous
		}
	}
	if kept["rating"] {
		result.Rating = current.Rating
	}
	result.LockedFields = current.LockedFields
	result.NameInitial = database.SeriesInitialFromNullTitle(result.Title, directoryName)
	return result
}

// SourceBacked names the scalar fields whose current value is still the value their source record
// holds, that is, a value an applied proposal or an identification's enrichment wrote and nobody
// changed since. Records the upgrade backfilled under "manual" name no source and back nothing.
// A rating matches its record exactly or as the one-decimal text proposals record.
func SourceBacked(current database.Series, records []database.SeriesMetadataProvenance) map[string]bool {
	values := map[string]sql.NullString{"title": current.Title, "summary": current.Summary, "publisher": current.Publisher, "status": current.Status, "language": current.Language}
	out := make(map[string]bool)
	for _, record := range records {
		if source := strings.TrimSpace(record.Source); source == "" || strings.EqualFold(source, "manual") || record.Value == "" {
			continue
		}
		if value, ok := values[record.FieldName]; ok {
			out[record.FieldName] = value.Valid && value.String == record.Value
			continue
		}
		if record.FieldName == "rating" && current.Rating.Valid {
			recorded, err := strconv.ParseFloat(record.Value, 64)
			out["rating"] = (err == nil && recorded == current.Rating.Float64) || strconv.FormatFloat(current.Rating.Float64, 'f', 1, 64) == record.Value
		}
	}
	return out
}

func lockedFields(value sql.NullString) map[string]bool {
	fields := make(map[string]bool)
	if value.Valid {
		for _, field := range strings.Split(value.String, ",") {
			if field = strings.TrimSpace(field); field != "" {
				fields[field] = true
			}
		}
	}
	return fields
}

func scanArchiveMetadata(info *parser.ComicInfo) ScanMetadata {
	var result ScanMetadata
	if info == nil {
		return result
	}
	result.Summary = sql.NullString{String: info.Summary, Valid: info.Summary != ""}
	result.Publisher = sql.NullString{String: info.Publisher, Valid: info.Publisher != ""}
	result.Language = sql.NullString{String: info.LanguageISO, Valid: info.LanguageISO != ""}
	if info.CommunityRating > 0 {
		result.Rating = sql.NullFloat64{Float64: float64(info.CommunityRating), Valid: true}
	}
	return result
}
