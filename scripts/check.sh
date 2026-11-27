#!/usr/bin/env bash
# 在本地复现 .github/workflows/ci.yml 的检查。用法：scripts/check.sh [--full]
# 默认档跑前端、静态分析、生成物漂移、文档与许可检查；--full 再加全量测试、race、CGO 构建、
# govulncheck 与 actionlint，推 main 或交付一批票前跑这一档。每步的完整输出写进
# .tmp/check/<步骤>.log，终端只打印失败步骤的摘要；最后一行固定是 `CHECK DONE exit=<n>`。

set -uo pipefail

full=0
case "${1:-}" in
  "") ;;
  --full) full=1 ;;
  *) echo "用法：scripts/check.sh [--full]" >&2; exit 2 ;;
esac

root=$(git rev-parse --show-toplevel) || exit 2
cd "$root" || exit 2
ci=.github/workflows/ci.yml

# 并行工作树共享主工作区的 Go 构建缓存（内容寻址、并发安全）；golangci-lint 的缓存把路径烤进了
# 结果，跨树共享会在源树删掉后回放出一屏假阳性，所以每棵树一份。
common=$(git rev-parse --path-format=absolute --git-common-dir)
export GOCACHE=${GOCACHE:-$(dirname "$common")/.gocache}
export GOTMPDIR=${GOTMPDIR:-$root/.tmp}
export GOLANGCI_LINT_CACHE=${GOLANGCI_LINT_CACHE:-$root/.tmp/golangci-cache}
logs=$root/.tmp/check
mkdir -p "$GOCACHE" "$GOTMPDIR" "$GOLANGCI_LINT_CACHE" "$logs"

# 工具版本以 CI 为准，从工作流里读，不在这里另记一份。
ci_version() { grep -oE "$1@v[0-9.]+" "$ci" | head -1 | sed 's/.*@//'; }

missing=()
for tool in go golangci-lint sqlc npm python3; do
  command -v "$tool" >/dev/null 2>&1 || missing+=("$tool")
done
if ((${#missing[@]})); then
  echo "PATH 里缺少：${missing[*]}（版本见 $ci）"
  echo "CHECK DONE exit=2"
  exit 2
fi
want_lint=$(grep -A3 'golangci-lint-action' "$ci" | grep 'version:' | grep -oE 'v[0-9]+\.[0-9]+\.[0-9]+' | head -1)
have_lint=$(golangci-lint --version 2>/dev/null | grep -oE '[0-9]+\.[0-9]+\.[0-9]+' | head -1)
[[ -n "$want_lint" && "v$have_lint" != "$want_lint" ]] && echo "注意：golangci-lint 是 v$have_lint，CI 用 $want_lint"
want_sqlc=$(ci_version 'sqlc/cmd/sqlc')
have_sqlc=$(sqlc version 2>/dev/null | tr -d '[:space:]')
[[ -n "$want_sqlc" && "$have_sqlc" != "$want_sqlc" ]] && echo "注意：sqlc 是 $have_sqlc，CI 用 $want_sqlc"

failed=()

# step <名字> <命令…>：跑一步，输出进日志，失败时只打印能定位问题的那几行。
step() {
  local name=$1; shift
  local log=$logs/$name.log start=$SECONDS
  printf '== %-14s ' "$name"
  if "$@" >"$log" 2>&1; then
    echo "ok ($((SECONDS - start))s)"
  else
    echo "FAIL ($((SECONDS - start))s)  完整输出：$log"
    if grep -qE -- '--- FAIL|^ *FAIL |^FAIL|^panic:' "$log"; then
      grep -E -- '--- FAIL|^ *FAIL |^FAIL|^panic:|_test\.go:[0-9]+:|^ *(Expected|Received):' "$log" | head -40
    else
      tail -30 "$log"
    fi
    failed+=("$name")
  fi
}

gofmt_clean() {
  local out
  out=$(gofmt -l ./cmd ./internal) || return 1
  [[ -z "$out" ]] || { echo "$out"; return 1; }
}

# drift <路径> <生成命令…>：跑生成器，看它有没有改动路径下的文件。与工作区原有改动对比，
# 所以没提交的手改不会被当成漂移；有漂移时生成结果留在工作区。
drift() {
  local path=$1; shift
  local before after status=0
  before=$(git status --porcelain -- "$path"; git diff -- "$path")
  "$@" || status=$?
  after=$(git status --porcelain -- "$path"; git diff -- "$path")
  if [[ "$before" != "$after" ]]; then
    echo "生成结果与工作区不一致，已重新生成："
    git status --porcelain -- "$path"
    return 1
  fi
  if ((status)); then
    # Git Bash 经 scoop shim 调 sqlc 会报假的退出码 1；产物无差异时以产物为准。
    case "$(uname -s)" in MINGW* | MSYS*) echo "生成器退出码 $status，产物无差异，按通过处理" ;; *) return "$status" ;; esac
  fi
}

# 拷来或久未重装的 node_modules 会和锁文件脱节，测出来的是旧依赖；先拦下来。
frontend() {
  (cd web && { npm ls --depth=0 >/dev/null || { echo "web/node_modules 与 package.json 不符，先在 web/ 跑 npm ci"; exit 1; }; } &&
    npm run lint && npm run test && npm run build)
}

# 前端先跑：它产出 Go 侧 go:embed 需要的 web/dist。
step frontend frontend
step gofmt gofmt_clean
step vet go vet ./...
step golangci-lint golangci-lint run --allow-serial-runners
step sqlc drift internal/database sqlc generate
step tsgen drift web/src/api/generated.ts go run ./cmd/tsgen
step doc-style ./scripts/check-doc-style.sh
step license python3 scripts/check-license-notices.py
step scripts-test python3 -B -m unittest discover -s scripts -p '*_test.py'

if ((full)); then
  step go-test go test -timeout=30m ./...
  step race go test -race -timeout=30m ./...
  step cgo-build env CGO_ENABLED=1 go build -o "$GOTMPDIR/yomifolio-ci" ./cmd/server
  step govulncheck env GOTOOLCHAIN=auto go run "golang.org/x/vuln/cmd/govulncheck@$(ci_version 'govulncheck')" ./...
  step actionlint env GOTOOLCHAIN=auto go run "github.com/rhysd/actionlint/cmd/actionlint@$(ci_version 'actionlint/cmd/actionlint')"
fi

if ((${#failed[@]})); then
  echo "失败：${failed[*]}"
  echo "CHECK DONE exit=1"
  exit 1
fi
echo "CHECK DONE exit=0"
