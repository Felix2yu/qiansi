#!/usr/bin/env bash
# 校验工具链声明在多处一致，避免 dependabot 只改 Dockerfile 基镜像导致运行时分叉。
# 涉及声明：.nvmrc（Node）、go.mod（Go）、Dockerfile 基镜像 tag（node:*/golang:*）。
# 若 web/package.json 写了 engines.node，也会一并比对。
set -euo pipefail

err=0
die() { echo "✗ $*" >&2; err=1; }

# ---- Node ----
nvmrc=$(cat .nvmrc 2>/dev/null | tr -d 'v' | tr -d '[:space:]')
node_docker=$(grep -oE 'node:[0-9]+(\.[0-9]+)?-alpine' Dockerfile | head -1 | grep -oE '[0-9]+(\.[0-9]+)?')
[ -z "$nvmrc" ] && die ".nvmrc 缺失或为空"
[ -z "$node_docker" ] && die "Dockerfile 中找不到 node:*-alpine 基镜像"
nvmrc_major=$(echo "$nvmrc" | cut -d. -f1)
node_docker_major=$(echo "$node_docker" | cut -d. -f1)
if [ "$nvmrc_major" != "$node_docker_major" ]; then
  die "Node 主版本不一致：.nvmrc=$nvmrc vs Dockerfile node:$node_docker"
fi
echo "✓ Node: .nvmrc=$nvmrc  Dockerfile node:$node_docker"

# ---- Go ----
go_mod=$(grep -oE '^go [0-9]+\.[0-9]+(\.[0-9]+)?' go.mod | head -1 | awk '{print $2}')
go_toolchain=$(grep -oE '^toolchain go[0-9]+\.[0-9]+\.[0-9]+' go.mod | head -1 | grep -oE '[0-9]+\.[0-9]+\.[0-9]+')
go_docker=$(grep -oE 'golang:[0-9]+\.[0-9]+-alpine' Dockerfile | head -1 | grep -oE '[0-9]+\.[0-9]+')
[ -z "$go_mod" ] && die "go.mod 中找不到 'go X.Y' 声明"
[ -z "$go_docker" ] && die "Dockerfile 中找不到 golang:*-alpine 基镜像"
go_mod_mm=$(echo "$go_mod" | cut -d. -f1,2)
if [ "$go_mod_mm" != "$go_docker" ]; then
  die "Go 版本不一致：go.mod go=$go_mod vs Dockerfile golang:$go_docker"
fi
echo "✓ Go: go.mod go=$go_mod${go_toolchain:+ (toolchain go$go_toolchain)}  Dockerfile golang:$go_docker"

# ---- 可选：web/package.json engines.node ----
if [ -f web/package.json ]; then
  engines=$(grep -oE '"node"\s*:\s*"[^"]+"' web/package.json | head -1 | grep -oE '[0-9]+' | head -1 || true)
  if [ -n "$engines" ]; then
    if [ "$engines" != "$node_docker_major" ]; then
      die "package.json engines.node=$engines 与 Dockerfile node:$node_docker 主版本不一致"
    fi
    echo "✓ web/package.json engines.node=$engines"
  fi
fi

if [ "$err" -ne 0 ]; then
  echo "" >&2
  echo "工具链声明分叉了。dependabot 提了 Docker 基镜像大版本时，请同时更新 .nvmrc / go.mod / web/package.json(engines)。" >&2
  exit 1
fi
echo ""
echo "工具链声明一致 ✓"
