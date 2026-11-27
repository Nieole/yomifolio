// Package sqliteconn 是 SQLite 连接参数的唯一来源：生产库和测试夹具都从这里取 DSN，
// 日志模式、同步级别与事务锁不会在两边各写一份而悄悄分叉。
package sqliteconn

// DSN 给 modernc SQLite 驱动的库文件路径加上生产连接参数。
//
// _txlock=immediate 让每个 BeginTx 发出 BEGIN IMMEDIATE，开始即取写锁。
// 连接池有多条可写连接（MaxOpenConns>=8），若用默认 deferred 事务，两条连接各自
// BEGIN→读→升级为写会撞上 SQLITE_BUSY_SNAPSHOT 死锁，busy_timeout 对这种“快照升级”
// 冲突无法重试，会立刻抛 database is locked。改为 immediate 后 busy_timeout 能干净地
// 串行化写者。database.Store 内 BeginTx 仅用于写事务（ExecTx 等），只读查询走 sqlc 直查 +
// WAL 并发，不受影响。
//
// 其余参数按百兆级的库取值：mmap_size 256MB 足以把整库映射进内存、消除换页压力；
// cache_size 128MB 页缓存；busy_timeout 15 秒，预防高并发读写撞上 busy lock；
// temp_store=2 让排序与聚合的临时表留在内存。
func DSN(path string) string {
	return path + "?_pragma=foreign_keys(1)&_pragma=journal_mode(WAL)&_pragma=synchronous(NORMAL)" +
		"&_pragma=mmap_size=268435456&_pragma=cache_size=-128000&_pragma=busy_timeout=15000&_pragma=temp_store=2" +
		"&_txlock=immediate"
}
