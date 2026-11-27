// Package sqlitetest 给测试开临时 SQLite 库，连接参数与生产一致（见 sqliteconn）。
//
// 测试夹具不得自配连接参数：默认的回滚日志下每次提交都要 fsync，磁盘一忙就把带截止时间的
// 用例拖成偶发红，而生产走 WAL，看不到这种延迟。.golangci.yml 的 forbidigo 因此禁止
// 测试直接调 sql.Open。
package sqlitetest

import (
	"database/sql"
	"path/filepath"
	"testing"

	_ "modernc.org/sqlite"

	"yomifolio/internal/database/sqliteconn"
)

// Open 在 t.TempDir() 下开名为 name 的库，测试结束时关闭。不迁移：建哪套表由调用方决定。
func Open(t testing.TB, name string) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite", sqliteconn.DSN(filepath.Join(t.TempDir(), name)))
	if err != nil {
		t.Fatalf("open sqlite %s: %v", name, err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db
}
