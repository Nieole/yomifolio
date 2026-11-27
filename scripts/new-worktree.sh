#!/usr/bin/env bash
# 建一棵并行干活用的工作树：scripts/new-worktree.sh <名字> <分支> [基点，默认 main]
# 树建在主工作区的 .claude/worktrees/<名字>；分支已存在就检出它，否则从基点新建。
# 拷入两样被忽略的东西：web/dist（web/web.go 的 go:embed 硬要它，缺了 go build 第一步就报
# pattern all:dist）和 web/node_modules（前端检查要它）。真要改前端的树得自己 npm run build。

set -euo pipefail

if (($# < 2 || $# > 3)); then
  echo "用法：scripts/new-worktree.sh <名字> <分支> [基点]" >&2
  exit 2
fi
name=$1 branch=$2 base=${3:-main}

main=$(dirname "$(git rev-parse --path-format=absolute --git-common-dir)")
tree=$main/.claude/worktrees/$name
if [[ ! -d "$main/web/dist" ]]; then
  echo "主工作区没有 web/dist，先在 $main/web 跑 npm run build" >&2
  exit 1
fi

if git -C "$main" show-ref --verify --quiet "refs/heads/$branch"; then
  git -C "$main" worktree add "$tree" "$branch"
else
  git -C "$main" worktree add -b "$branch" "$tree" "$base"
fi
cp -a "$main/web/dist" "$tree/web/dist"
if [[ -d "$main/web/node_modules" ]]; then
  cp -a "$main/web/node_modules" "$tree/web/node_modules"
else
  echo "注意：主工作区没有 web/node_modules，这棵树的前端检查要先 npm ci"
fi
mkdir -p "$tree/.tmp"

echo "ready: $tree on $(git -C "$tree" rev-parse --abbrev-ref HEAD) @ $(git -C "$tree" rev-parse --short HEAD)"
echo "磁盘剩余：$(df -h "$tree" | awk 'NR == 2 {print $4}')，共享的 .gocache 在 $main/.gocache"
