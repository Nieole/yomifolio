// 本文件由 store.go 拆分而来，属于 SQLite 数据访问层的「系列搜索与分页」子域。
// 它承载索引候选与精确字段匹配、offset/cursor 双分页、筛选条件拼装与游标编解码。
// 维护时应保证 SQL 全部参数化、排序字段走白名单，并让新增排序同时覆盖 offset 与 cursor 两条路径。

package database

import (
	"context"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"
)

// sqliteDatetimeLayout 是 series.created_at / updated_at 的库内存储文本格式：这两列只由
// CURRENT_TIMESTAMP 写入，即 UTC、秒精度、无时区后缀。任何要与这两列比大小的查询参数都得按它
// 绑定——SQLite 比的是文本，绑 time.Time 会被驱动写成另一种写法而比错。
const sqliteDatetimeLayout = "2006-01-02 15:04:05"

type SearchSeriesPagedRow struct {
	Series
	CoverPath       sql.NullString  `json:"cover_path"`
	TagsString      *string         `json:"tags_string"`
	VolumeCount     int             `json:"volume_count"`
	ActualBookCount int             `json:"actual_book_count"`
	ReadCount       int             `json:"read_count"`
	TotalPages      sql.NullFloat64 `json:"total_pages"`
	IsFavorite      bool            `json:"is_favorite"`
	LastReadAt      sql.NullTime    `json:"last_read_at"`
	LastReadBookID  sql.NullInt64   `json:"last_read_book_id"`
	LastReadPage    sql.NullInt64   `json:"last_read_page"`
}

type SeriesSearchHit struct {
	SearchSeriesPagedRow
	Score float64
	Rank  SearchMatchRank
}

type BookSearchHit struct {
	Book
	SeriesName  string
	SeriesTitle sql.NullString
	Score       float64
	Rank        SearchMatchRank
}

type seriesSearchSort struct {
	Field string
	Dir   string
	Expr  string
}

// ErrSeriesCursorUnusable 标记「这个游标用不了」：排序已换、串坏了、载荷不合法，或该排序根本
// 不支持游标。四者都只说明客户端手里的游标过期或无效，调用方应忽略游标改走 offset 分页，
// 而不是让整个请求失败。真正的服务端故障（SQL 执行、扫描行）不带这个标记。
var ErrSeriesCursorUnusable = errors.New("series cursor unusable")

type seriesCursorPayload struct {
	SortBy string `json:"sort_by"`
	Value  string `json:"value"`
	Name   string `json:"name"`
	ID     int64  `json:"id"`
}

type ProtocolSeriesRow struct {
	ID          int64     `json:"id"`
	LibraryID   int64     `json:"library_id"`
	Name        string    `json:"name"`
	Title       string    `json:"title"`
	Summary     string    `json:"summary"`
	Status      string    `json:"status"`
	BookCount   int64     `json:"book_count"`
	TotalPages  int64     `json:"total_pages"`
	CoverPath   string    `json:"cover_path"`
	CoverBookID int64     `json:"cover_book_id"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

// SearchSeriesPaged 供主页根据标签和作者查询并分页。
// 默认列表只走 series + series_stats，只有标签/作者筛选时才进入关联表。
func (s *SqlStore) SearchSeriesPaged(ctx context.Context, libraryID int64, f SeriesListFilters, limit, offset int32, sortBy string) ([]SearchSeriesPagedRow, int, error) {
	baseQuery, progressJoin, whereClause, leadingArgs, args, whereUsesProgress := buildSeriesSearchQuery(libraryID, f)

	var total int
	if !f.hasAny() {
		if libraryID == 0 {
			if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM series`).Scan(&total); err != nil {
				return nil, 0, err
			}
		} else if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM series WHERE library_id = ?`, libraryID).Scan(&total); err != nil {
			return nil, 0, err
		}
	} else {
		// 计数仅在 WHERE 引用 ss.*（阅读状态/进度筛选）时才 JOIN 进度来源；否则省掉该 LEFT JOIN
		// （ss 主键点查每系列至多一行、不放大行数，结果一致）。JOIN 若含 user_id 占位符，
		// 需先补其实参（f.UserID>0 时），再接 WHERE 实参。
		countArgs := make([]interface{}, 0, len(args)+1)
		countJoin := ""
		if whereUsesProgress {
			countJoin = progressJoin
			if f.UserID > 0 {
				countArgs = append(countArgs, f.UserID)
			}
		}
		countArgs = append(countArgs, args...)
		countQuery := `SELECT COUNT(*) FROM series s ` + countJoin + whereClause
		if err := s.db.QueryRowContext(ctx, countQuery, countArgs...).Scan(&total); err != nil {
			return nil, 0, err
		}
	}

	sortSpec := parseSeriesSearchSort(sortBy)
	orderClause := seriesSearchOffsetOrderClause(sortSpec)

	queryArgs := append(append([]interface{}{}, leadingArgs...), args...)
	baseQuery += whereClause + fmt.Sprintf(` ORDER BY %s LIMIT ? OFFSET ?`, orderClause)
	queryArgs = append(queryArgs, limit, offset)

	rows, err := s.db.QueryContext(ctx, baseQuery, queryArgs...)
	if err != nil {
		return nil, 0, err
	}
	items, err := scanSearchSeriesPagedRows(rows)
	if err != nil {
		return nil, 0, err
	}
	return items, total, nil
}

func (s *SqlStore) SearchSeriesCursor(ctx context.Context, libraryID int64, f SeriesListFilters, limit int32, sortBy, cursor string) ([]SearchSeriesPagedRow, string, bool, error) {
	sortSpec := parseSeriesSearchSort(sortBy)
	if !sortSpec.supportsCursor() {
		return nil, "", false, fmt.Errorf("%w: sort %q does not support cursor pagination", ErrSeriesCursorUnusable, sortBy)
	}
	if limit <= 0 {
		limit = 50
	}

	baseQuery, _, whereClause, leadingArgs, args, _ := buildSeriesSearchQuery(libraryID, f)
	filters := strings.TrimPrefix(whereClause, " WHERE ")
	queryArgs := append(append([]interface{}{}, leadingArgs...), args...)

	if cursor != "" {
		payload, err := decodeSeriesCursor(cursor)
		if err != nil {
			return nil, "", false, err
		}
		if payload.SortBy != seriesSearchSortKey(sortSpec) {
			return nil, "", false, fmt.Errorf("%w: cursor sort %q does not match request sort %q", ErrSeriesCursorUnusable, payload.SortBy, seriesSearchSortKey(sortSpec))
		}
		seekClause, seekArgs := seriesSearchSeekClause(sortSpec, payload)
		if seekClause != "" {
			if filters != "" {
				filters += " AND "
			}
			filters += seekClause
			queryArgs = append(queryArgs, seekArgs...)
		}
	}

	if filters != "" {
		baseQuery += " WHERE " + filters
	}
	orderClause := seriesSearchCursorOrderClause(sortSpec)
	queryArgs = append(queryArgs, int(limit)+1)
	baseQuery += fmt.Sprintf(` ORDER BY %s LIMIT ?`, orderClause)

	rows, err := s.db.QueryContext(ctx, baseQuery, queryArgs...)
	if err != nil {
		return nil, "", false, err
	}
	items, err := scanSearchSeriesPagedRows(rows)
	if err != nil {
		return nil, "", false, err
	}

	hasMore := len(items) > int(limit)
	if hasMore {
		items = items[:int(limit)]
	}
	nextCursor := ""
	if hasMore && len(items) > 0 {
		nextCursor = encodeSeriesCursor(sortSpec, items[len(items)-1])
	}
	return items, nextCursor, hasMore, nil
}

func (s *SqlStore) SearchGlobalSeries(ctx context.Context, keyword string, limit int32) ([]SeriesSearchHit, error) {
	keyword = NormalizeSearchText(keyword)
	if keyword == "" {
		return []SeriesSearchHit{}, nil
	}
	if limit <= 0 {
		limit = 20
	}
	return s.searchGlobalSeriesSubstring(ctx, keyword, limit)
}

func (s *SqlStore) SearchGlobalBooks(ctx context.Context, keyword string, limit int32) ([]BookSearchHit, error) {
	keyword = NormalizeSearchText(keyword)
	if keyword == "" {
		return []BookSearchHit{}, nil
	}
	if limit <= 0 {
		limit = 20
	}

	return s.searchGlobalBooksSubstring(ctx, keyword, limit)
}

func (s *SqlStore) SearchProtocolSeries(ctx context.Context, keyword string, limit, offset int32) ([]ProtocolSeriesRow, int, error) {
	keyword = NormalizeSearchText(keyword)
	if keyword == "" {
		return []ProtocolSeriesRow{}, 0, nil
	}
	if limit <= 0 {
		limit = 30
	}
	if offset < 0 {
		offset = 0
	}
	return s.searchProtocolSeriesSubstring(ctx, keyword, limit, offset)
}

func (s *SqlStore) searchProtocolSeriesSubstring(ctx context.Context, keyword string, limit, offset int32) ([]ProtocolSeriesRow, int, error) {
	filter, args := normalizedSeriesFilter(keyword)
	var total int
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM series s WHERE `+filter, args...).Scan(&total); err != nil {
		return nil, 0, err
	}
	rankJoin, rankOrder := "", ""
	queryArgs := []interface{}{}
	if len([]rune(keyword)) >= 3 {
		rankJoin = `LEFT JOIN (SELECT rowid, bm25(series_search_fts, 2.0, 3.0) AS rank FROM series_search_fts WHERE series_search_fts MATCH ?) m ON m.rowid = s.id`
		rankOrder = "m.rank ASC, "
		queryArgs = append(queryArgs, fts5PhraseQuery(keyword))
	}
	queryArgs = append(queryArgs, args...)
	queryArgs = append(queryArgs, keyword, keyword, keyword, keyword, limit, offset)

	rows, err := s.db.QueryContext(ctx, `
		SELECT
			s.id,
			s.library_id,
			s.name,
			COALESCE(s.title, '') AS title,
			COALESCE(s.summary, '') AS summary,
			COALESCE(s.status, '') AS status,
			s.book_count,
			s.total_pages,
			COALESCE(ss.cover_path, '') AS cover_path,
			COALESCE(ss.cover_book_id, 0) AS cover_book_id,
			s.created_at,
			s.updated_at
		FROM series s
		JOIN series_search_text st ON st.series_id = s.id
		LEFT JOIN series_stats ss ON ss.series_id = s.id
		`+rankJoin+`
		WHERE `+filter+`
		ORDER BY
			CASE
				WHEN st.name = search_normalize(?) OR st.title = search_normalize(?) THEN 0
				WHEN instr(st.name, search_normalize(?)) > 0 OR instr(st.title, search_normalize(?)) > 0 THEN 1
				ELSE 2
			END,
			`+rankOrder+`COALESCE(NULLIF(s.title, ''), s.name) COLLATE NOCASE ASC,
			s.id ASC
		LIMIT ? OFFSET ?
	`, queryArgs...)
	if err != nil {
		return nil, 0, err
	}
	items, err := scanProtocolSeriesRows(rows)
	return items, total, err
}

// Global candidates are reduced by identity only after choosing the best field.
// Each type is ordered by the same key before LIMIT: its top N is a sufficient
// superset of its contribution to the mixed top N (no raw-source truncation).
func (s *SqlStore) searchGlobalBooksSubstring(ctx context.Context, keyword string, limit int32) ([]BookSearchHit, error) {
	seriesFilter, seriesArgs := normalizedSeriesFilter(keyword)
	table, match := "book_search_fts", fts5PhraseQuery(keyword)
	if expr, ok := normalizedBookGramExpr(keyword); ok {
		table, match = "book_gram_fts", expr
	}
	candidates := `SELECT rowid AS id FROM ` + table + ` WHERE ` + table + ` MATCH ?
 UNION SELECT b2.id FROM series s JOIN books b2 ON b2.series_id=s.id WHERE ` + seriesFilter
	args := append([]interface{}{match}, seriesArgs...)
	fields := `SELECT c.id, bt.name AS field, 0 AS inherited FROM candidates c JOIN book_search_text bt ON bt.book_id=c.id
 UNION ALL SELECT c.id, bt.title, 0 FROM candidates c JOIN book_search_text bt ON bt.book_id=c.id
 UNION ALL SELECT c.id, st.name, 1 FROM candidates c JOIN books b ON b.id=c.id JOIN series_search_text st ON st.series_id=b.series_id
 UNION ALL SELECT c.id, st.title, 1 FROM candidates c JOIN books b ON b.id=c.id JOIN series_search_text st ON st.series_id=b.series_id`
	args = append(args, keyword, keyword, keyword, keyword, limit)
	rows, err := s.db.QueryContext(ctx, globalRankCTE(candidates, fields)+`
 SELECT b.id, b.series_id, b.library_id, b.name, b.path, b.size, b.file_modified_at, b.volume, b.title, b.summary, b.number, b.sort_number, b.page_count, b.cover_path, b.last_read_page, b.last_read_at, b.is_read, b.file_hash, b.quick_hash, b.path_fingerprint, b.path_fingerprint_no_ext, b.filename_fingerprint, b.created_at, b.updated_at,
 s.name, s.title, r.tier, r.position, r.extra
 FROM ranked r JOIN books b ON b.id=r.id JOIN series s ON s.id=b.series_id
 WHERE r.choice=1 ORDER BY r.tier,r.position,r.extra,r.id LIMIT ?`, args...)
	if err != nil {
		return nil, err
	}
	return scanBookSearchHits(rows)
}

func (s *SqlStore) searchGlobalSeriesSubstring(ctx context.Context, keyword string, limit int32) ([]SeriesSearchHit, error) {
	filter, args := normalizedSeriesFilter(keyword)
	candidates := `SELECT s.id FROM series s WHERE ` + filter
	fields := `SELECT c.id, st.name AS field, 0 AS inherited FROM candidates c JOIN series_search_text st ON st.series_id=c.id
 UNION ALL SELECT c.id, st.title, 0 FROM candidates c JOIN series_search_text st ON st.series_id=c.id`
	args = append(args, keyword, keyword, keyword, keyword, limit)
	rows, err := s.db.QueryContext(ctx, globalRankCTE(candidates, fields)+`
 SELECT s.id, s.library_id, s.name, s.title, s.summary, s.publisher, s.status, s.rating, s.language, s.locked_fields, s.name_initial, s.path, s.created_at, s.updated_at, s.is_favorite, s.volume_count, s.book_count, s.total_pages,
 ss.cover_path, COALESCE(ss.tag_names_cache, ''), COALESCE(ss.read_pages, 0),ss.last_read_at,NULLIF(ss.last_read_book_id,0),
 (SELECT b2.last_read_page FROM books b2 WHERE b2.id=ss.last_read_book_id), r.tier,r.position,r.extra
 FROM ranked r JOIN series s ON s.id=r.id LEFT JOIN series_stats ss ON ss.series_id=s.id
 WHERE r.choice=1 ORDER BY r.tier,r.position,r.extra,r.id LIMIT ?`, args...)
	if err != nil {
		return nil, err
	}
	return scanSeriesSearchHits(rows)
}

// SearchMatchRank contains no text. SQLite's maximum TEXT length is below 2^31
// bytes, so character positions and excess lengths fit uint32 without saturation
// or truncation. Identity and kind complete this key in catalogsearch.
type SearchMatchRank struct {
	Tier     uint8
	Position uint32
	Extra    uint32
}

func (r SearchMatchRank) Score() float64 {
	switch r.Tier {
	case 0:
		return 1
	case 1:
		return 0.5
	default:
		return 0.25
	}
}

func globalRankCTE(candidates, fields string) string {
	return `WITH candidates AS (` + candidates + `), fields AS (` + fields + `),
 matches AS (SELECT id, CASE WHEN inherited=1 THEN 2 WHEN field=? THEN 0 ELSE 1 END AS tier,
 instr(field,?)-1 AS position, length(field)-length(?) AS extra FROM fields WHERE instr(field,?)>0),
 ranked AS (SELECT *, row_number() OVER (PARTITION BY id ORDER BY tier,position,extra, id ASC) AS choice FROM matches)`
}

func fts5PhraseQuery(keyword string) string {
	return `"` + strings.ReplaceAll(strings.TrimSpace(keyword), `"`, `""`) + `"`
}

// SeriesListFilters 收敛资料库/系列列表查询的全部可选筛选条件，避免多参数签名膨胀。
// 阅读状态 / 评分区间 / 进度区间 / 加入天数为 2026-07 新增（对齐智能合集筛选维度）；
// 零值字段（空串 / nil 指针 / 0）表示“不筛选该维度”。
type SeriesListFilters struct {
	Keyword string
	Letter  string
	Status  string
	Tags    []string
	Authors []string
	// ReadState: "" | "unread"（未读）| "reading"（在读）| "completed"（已读完）。
	ReadState string
	// 评分区间（series.rating，NULL 评分不满足任一边界，天然被排除）。
	MinRating *float64
	MaxRating *float64
	// 进度区间百分比 0–100（read_pages / total_pages）。
	MinProgress *float64
	MaxProgress *float64
	// 仅保留最近 N 天内加入的系列；0 表示不限。
	AddedWithinDays int
	// UserID>0 时进度来源为该用户的 user_series_progress（多用户）；0 表示全局 series_stats。
	// 不计入 hasAny()——它只切换进度来源，不构成过滤条件。
	UserID int64
}

// hasAny 报告是否存在任一筛选条件，用于决定计数是否走无筛选快路径。
func (f SeriesListFilters) hasAny() bool {
	return f.Keyword != "" || f.Letter != "" || f.Status != "" || len(f.Tags) > 0 || len(f.Authors) > 0 ||
		f.ReadState != "" || f.MinRating != nil || f.MaxRating != nil || f.MinProgress != nil || f.MaxProgress != nil ||
		f.AddedWithinDays > 0
}

// buildSeriesSearchQuery 组装系列列表/搜索查询。别名约定：sc = series_stats（全局封面/标签缓存），
// ss = 进度来源（f.UserID>0 时为该用户的 user_series_progress，否则为全局 series_stats）。
// 返回：baseQuery（含 SELECT + FROM/JOIN）、progressJoin（ss 的 JOIN 子句，供计数查询复用）、
// whereClause、leadingArgs（baseQuery 中 WHERE 之前出现的占位符实参，按文本顺序）、whereArgs（过滤实参）。
func buildSeriesSearchQuery(libraryID int64, f SeriesListFilters) (baseQuery, progressJoin, whereClause string, leadingArgs, whereArgs []interface{}, whereUsesProgress bool) {
	// last_read_page 取「最近阅读那本书」的进度：多用户从 user_book_progress，否则从全局 books。
	var lastReadPageExpr string
	leadingArgs = make([]interface{}, 0, 2)
	if f.UserID > 0 {
		lastReadPageExpr = `(SELECT ubp.last_read_page FROM user_book_progress ubp WHERE ubp.book_id = ss.last_read_book_id AND ubp.user_id = ?)`
		leadingArgs = append(leadingArgs, f.UserID) // SELECT 子查询里的 user_id
		progressJoin = `LEFT JOIN user_series_progress ss ON ss.series_id = s.id AND ss.user_id = ?`
		leadingArgs = append(leadingArgs, f.UserID) // progressJoin 里的 user_id
	} else {
		lastReadPageExpr = `(SELECT b2.last_read_page FROM books b2 WHERE b2.id = ss.last_read_book_id)`
		progressJoin = `LEFT JOIN series_stats ss ON ss.series_id = s.id`
	}

	baseQuery = `
		SELECT
            s.id, s.library_id, s.name, s.title, s.summary, s.publisher, s.status, s.rating, s.language, s.locked_fields, s.name_initial, s.path, s.created_at, s.updated_at, s.is_favorite, s.volume_count, s.book_count, s.total_pages,
            sc.cover_path,
            COALESCE(sc.tag_names_cache, '') as tags_string,
            COALESCE(ss.read_pages, 0) as read_count,
            ss.last_read_at,
            NULLIF(ss.last_read_book_id, 0) AS last_read_book_id,
            ` + lastReadPageExpr + ` AS last_read_page
		FROM series s
		LEFT JOIN series_stats sc ON sc.series_id = s.id
		` + progressJoin + `
	`

	filters := make([]string, 0, 8)
	args := make([]interface{}, 0, 12)
	if libraryID != 0 {
		filters = append(filters, `s.library_id = ?`)
		args = append(args, libraryID)
	}

	if f.Keyword != "" {
		filter, filterArgs := normalizedSeriesFilter(f.Keyword)
		filters = append(filters, filter)
		args = append(args, filterArgs...)
	}

	if f.Status != "" {
		filters = append(filters, `s.status = ?`)
		args = append(args, f.Status)
	}

	if f.Letter != "" {
		normalizedLetter := strings.ToUpper(strings.TrimSpace(f.Letter))
		if normalizedLetter != "" {
			if normalizedLetter == "#" {
				filters = append(filters, `s.name_initial = '#'`)
			} else {
				filters = append(filters, `s.name_initial = ?`)
				args = append(args, normalizedLetter)
			}
		}
	}

	if len(f.Tags) > 0 {
		filter := `EXISTS (
			SELECT 1
			FROM series_tags st
			JOIN tags t ON st.tag_id = t.id
			WHERE st.series_id = s.id AND t.name IN (`
		for i, tag := range f.Tags {
			if i > 0 {
				filter += `, `
			}
			filter += `?`
			args = append(args, tag)
		}
		filter += `))`
		filters = append(filters, filter)
	}

	if len(f.Authors) > 0 {
		filter := `EXISTS (
			SELECT 1
			FROM series_authors sa
			JOIN authors a ON sa.author_id = a.id
			WHERE sa.series_id = s.id AND a.name IN (`
		for i, author := range f.Authors {
			if i > 0 {
				filter += `, `
			}
			filter += `?`
			args = append(args, author)
		}
		filter += `))`
		filters = append(filters, filter)
	}

	// 阅读状态：基于 series_stats 的已读/读完卷册数与 series.book_count。
	switch f.ReadState {
	case "unread":
		filters = append(filters, `COALESCE(ss.read_book_count, 0) = 0`)
	case "reading":
		filters = append(filters, `COALESCE(ss.read_book_count, 0) > 0 AND COALESCE(ss.completed_book_count, 0) < s.book_count`)
	case "completed":
		filters = append(filters, `s.book_count > 0 AND COALESCE(ss.completed_book_count, 0) >= s.book_count`)
	}

	if f.MinRating != nil {
		filters = append(filters, `s.rating >= ?`)
		args = append(args, *f.MinRating)
	}
	if f.MaxRating != nil {
		filters = append(filters, `s.rating <= ?`)
		args = append(args, *f.MaxRating)
	}

	// 进度百分比 = read_pages / total_pages * 100；总页数为 0 时视作 0%。
	progressExpr := `COALESCE(CAST(COALESCE(ss.read_pages, 0) AS REAL) / NULLIF(s.total_pages, 0) * 100, 0)`
	if f.MinProgress != nil {
		filters = append(filters, progressExpr+` >= ?`)
		args = append(args, *f.MinProgress)
	}
	if f.MaxProgress != nil {
		filters = append(filters, progressExpr+` <= ?`)
		args = append(args, *f.MaxProgress)
	}

	if f.AddedWithinDays > 0 {
		cutoff := time.Now().UTC().AddDate(0, 0, -f.AddedWithinDays).Format(sqliteDatetimeLayout)
		filters = append(filters, `s.created_at >= ?`)
		args = append(args, cutoff)
	}

	whereClause = ""
	if len(filters) > 0 {
		whereClause = " WHERE " + strings.Join(filters, " AND ")
	}
	// whereUsesProgress 标记 WHERE 是否引用进度来源 ss.*（阅读状态 / 进度区间筛选）。计数查询据此决定
	// 是否 JOIN 进度来源：ss 经主键点查每系列至多一行、LEFT JOIN 不放大行数，故 WHERE 不引用 ss 时
	// 可从 COUNT(*) 省掉该 JOIN（结果不变，少一次 join）。
	whereUsesProgress = f.ReadState == "unread" || f.ReadState == "reading" || f.ReadState == "completed" || f.MinProgress != nil || f.MaxProgress != nil
	return baseQuery, progressJoin, whereClause, leadingArgs, args, whereUsesProgress
}

func scanSearchSeriesPagedRows(rows *sql.Rows) ([]SearchSeriesPagedRow, error) {
	defer rows.Close()

	var items []SearchSeriesPagedRow
	for rows.Next() {
		var i SearchSeriesPagedRow
		if err := rows.Scan(
			&i.ID,
			&i.LibraryID,
			&i.Name,
			&i.Title,
			&i.Summary,
			&i.Publisher,
			&i.Status,
			&i.Rating,
			&i.Language,
			&i.LockedFields,
			&i.NameInitial,
			&i.Path,
			&i.CreatedAt,
			&i.UpdatedAt,
			&i.IsFavorite,
			&i.VolumeCount,
			&i.BookCount,
			&i.TotalPages,
			&i.CoverPath,
			&i.TagsString,
			&i.ReadCount,
			&i.LastReadAt,
			&i.LastReadBookID,
			&i.LastReadPage,
		); err != nil {
			return nil, err
		}
		i.ActualBookCount = int(i.BookCount)
		items = append(items, i)
	}
	if err := rows.Close(); err != nil {
		return nil, err
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return items, nil
}

func scanSeriesSearchHits(rows *sql.Rows) ([]SeriesSearchHit, error) {
	defer rows.Close()

	var items []SeriesSearchHit
	for rows.Next() {
		var i SeriesSearchHit
		if err := rows.Scan(
			&i.ID,
			&i.LibraryID,
			&i.Name,
			&i.Title,
			&i.Summary,
			&i.Publisher,
			&i.Status,
			&i.Rating,
			&i.Language,
			&i.LockedFields,
			&i.NameInitial,
			&i.Path,
			&i.CreatedAt,
			&i.UpdatedAt,
			&i.IsFavorite,
			&i.VolumeCount,
			&i.BookCount,
			&i.TotalPages,
			&i.CoverPath,
			&i.TagsString,
			&i.ReadCount,
			&i.LastReadAt,
			&i.LastReadBookID,
			&i.LastReadPage,
			&i.Rank.Tier,
			&i.Rank.Position,
			&i.Rank.Extra,
		); err != nil {
			return nil, err
		}
		i.ActualBookCount = int(i.BookCount)
		i.Score = i.Rank.Score()
		items = append(items, i)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return items, nil
}

func scanBookSearchHits(rows *sql.Rows) ([]BookSearchHit, error) {
	defer rows.Close()

	var items []BookSearchHit
	for rows.Next() {
		var i BookSearchHit
		if err := rows.Scan(
			&i.ID,
			&i.SeriesID,
			&i.LibraryID,
			&i.Name,
			&i.Path,
			&i.Size,
			&i.FileModifiedAt,
			&i.Volume,
			&i.Title,
			&i.Summary,
			&i.Number,
			&i.SortNumber,
			&i.PageCount,
			&i.CoverPath,
			&i.LastReadPage,
			&i.LastReadAt,
			&i.IsRead,
			&i.FileHash,
			&i.QuickHash,
			&i.PathFingerprint,
			&i.PathFingerprintNoExt,
			&i.FilenameFingerprint,
			&i.CreatedAt,
			&i.UpdatedAt,
			&i.SeriesName,
			&i.SeriesTitle,
			&i.Rank.Tier,
			&i.Rank.Position,
			&i.Rank.Extra,
		); err != nil {
			return nil, err
		}
		i.Score = i.Rank.Score()
		items = append(items, i)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return items, nil
}

func scanProtocolSeriesRows(rows *sql.Rows) ([]ProtocolSeriesRow, error) {
	defer rows.Close()

	var items []ProtocolSeriesRow
	for rows.Next() {
		var item ProtocolSeriesRow
		if err := rows.Scan(
			&item.ID,
			&item.LibraryID,
			&item.Name,
			&item.Title,
			&item.Summary,
			&item.Status,
			&item.BookCount,
			&item.TotalPages,
			&item.CoverPath,
			&item.CoverBookID,
			&item.CreatedAt,
			&item.UpdatedAt,
		); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return items, nil
}

func parseSeriesSearchSort(sortBy string) seriesSearchSort {
	spec := seriesSearchSort{Field: "name", Dir: "ASC", Expr: "s.name"}
	parts := strings.Split(sortBy, "_")
	if len(parts) != 2 {
		return spec
	}

	field, dir := parts[0], strings.ToUpper(parts[1])
	if dir != "ASC" && dir != "DESC" {
		dir = "ASC"
	}
	spec.Field = field
	spec.Dir = dir
	switch field {
	case "rating":
		spec.Expr = "s.rating"
	case "books":
		spec.Expr = "s.book_count"
	case "volumes":
		spec.Expr = "s.volume_count"
	case "pages":
		spec.Expr = "s.total_pages"
	case "read":
		spec.Expr = "COALESCE(ss.read_pages, 0)"
	case "created":
		spec.Expr = "s.created_at"
	case "updated":
		spec.Expr = "s.updated_at"
	case "favorite":
		spec.Expr = "s.is_favorite"
	case "name":
		spec.Expr = "s.name"
	default:
		spec.Field = "name"
		spec.Expr = "s.name"
	}
	return spec
}

func (s seriesSearchSort) supportsCursor() bool {
	switch s.Field {
	// books/volumes/pages 均为 NOT NULL 整数列，方向匹配的 *_desc 复合索引已具备，(name,id) 保证 tie-break
	// 唯一稳定，故可 keyset 前滚跳过 COUNT 与深 OFFSET。rating 可空（NULL 在 keyset 边界谓词里会漏行）故不纳入。
	case "name", "updated", "created", "favorite", "books", "volumes", "pages":
		return true
	default:
		return false
	}
}

func SeriesSearchSortSupportsCursor(sortBy string) bool {
	return parseSeriesSearchSort(sortBy).supportsCursor()
}

func NextSeriesSearchCursor(sortBy string, row SearchSeriesPagedRow) string {
	sortSpec := parseSeriesSearchSort(sortBy)
	if !sortSpec.supportsCursor() {
		return ""
	}
	return encodeSeriesCursor(sortSpec, row)
}

func seriesSearchSortKey(s seriesSearchSort) string {
	return s.Field + "_" + strings.ToLower(s.Dir)
}

func seriesSearchOffsetOrderClause(s seriesSearchSort) string {
	switch s.Field {
	case "rating", "books", "volumes", "pages", "read", "created", "updated":
		return fmt.Sprintf("%s %s, s.name ASC, s.id ASC", s.Expr, s.Dir)
	case "favorite":
		return fmt.Sprintf("s.is_favorite %s, s.name ASC, s.id ASC", s.Dir)
	case "name":
		return fmt.Sprintf("s.name %s, s.id %s", s.Dir, s.Dir)
	default:
		return "s.name ASC, s.id ASC"
	}
}

func seriesSearchCursorOrderClause(s seriesSearchSort) string {
	if s.Field == "name" {
		return fmt.Sprintf("s.name %s, s.id %s", s.Dir, s.Dir)
	}
	return fmt.Sprintf("%s %s, s.name ASC, s.id ASC", s.Expr, s.Dir)
}

func seriesSearchSeekClause(s seriesSearchSort, cursor seriesCursorPayload) (string, []interface{}) {
	if s.Field == "name" {
		operator := ">"
		if s.Dir == "DESC" {
			operator = "<"
		}
		return fmt.Sprintf(`(s.name %s ? OR (s.name = ? AND s.id %s ?))`, operator, operator), []interface{}{cursor.Name, cursor.Name, cursor.ID}
	}

	operator := ">"
	if s.Dir == "DESC" {
		operator = "<"
	}
	value := interface{}(cursor.Value)
	switch s.Field {
	case "updated", "created":
		value = seriesDatetimeSeekValue(cursor.Value)
	case "favorite", "books", "volumes", "pages":
		if parsed, err := strconv.Atoi(cursor.Value); err == nil {
			value = parsed
		}
	}
	return fmt.Sprintf(`(%s %s ? OR (%s = ? AND (s.name > ? OR (s.name = ? AND s.id > ?))))`, s.Expr, operator, s.Expr),
		[]interface{}{value, value, cursor.Name, cursor.Name, cursor.ID}
}

// seriesDatetimeSeekValue 把游标里的时间值还原成 sqliteDatetimeLayout 文本，供键集边界谓词绑定。
// 换成 time.Time 会让边界判错：倒序把边界行再发一遍，正序漏掉与边界同一秒的行。
func seriesDatetimeSeekValue(raw string) interface{} {
	parsed, err := time.Parse(time.RFC3339Nano, raw)
	if err != nil {
		return raw
	}
	return parsed.UTC().Format(sqliteDatetimeLayout)
}

func encodeSeriesCursor(s seriesSearchSort, row SearchSeriesPagedRow) string {
	payload := seriesCursorPayload{
		SortBy: seriesSearchSortKey(s),
		Name:   row.Name,
		ID:     row.ID,
	}
	switch s.Field {
	case "updated":
		payload.Value = row.UpdatedAt.Format(time.RFC3339Nano)
	case "created":
		payload.Value = row.CreatedAt.Format(time.RFC3339Nano)
	case "favorite":
		if row.IsFavorite {
			payload.Value = "1"
		} else {
			payload.Value = "0"
		}
	case "books":
		payload.Value = strconv.FormatInt(row.BookCount, 10)
	case "volumes":
		payload.Value = strconv.Itoa(row.VolumeCount)
	case "pages":
		if row.TotalPages.Valid {
			payload.Value = strconv.FormatInt(int64(row.TotalPages.Float64), 10)
		} else {
			payload.Value = "0"
		}
	default:
		payload.Value = row.Name
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		return ""
	}
	return base64.RawURLEncoding.EncodeToString(raw)
}

func decodeSeriesCursor(cursor string) (seriesCursorPayload, error) {
	var payload seriesCursorPayload
	raw, err := base64.RawURLEncoding.DecodeString(cursor)
	if err != nil {
		return payload, fmt.Errorf("%w: base64: %v", ErrSeriesCursorUnusable, err)
	}
	if err := json.Unmarshal(raw, &payload); err != nil {
		return payload, fmt.Errorf("%w: json: %v", ErrSeriesCursorUnusable, err)
	}
	if payload.SortBy == "" || payload.ID <= 0 {
		return payload, fmt.Errorf("%w: missing sort key or invalid id", ErrSeriesCursorUnusable)
	}
	return payload, nil
}
