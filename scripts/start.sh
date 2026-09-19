#!/bin/bash
# 一键启动局域网共享（Mac / Linux）。
# 有 Go 就从源码编译；没有 Go 就从本仓库的 GitHub Releases 下载编译好的程序。
# 参数会原样传给程序，例如：./scripts/start.sh -no-password
set -euo pipefail
cd "$(dirname "$0")/.."

BIN="bin/lanshare"
mkdir -p bin

if command -v go >/dev/null 2>&1; then
  go build -o "$BIN" ./cmd/lanshare
elif [ ! -x "$BIN" ]; then
  os=$(uname -s | tr '[:upper:]' '[:lower:]')
  arch=$(uname -m); [ "$arch" = "x86_64" ] && arch=amd64; [ "$arch" = "aarch64" ] && arch=arm64
  # 从 git remote 推出仓库地址，例如 git@github.com:foo/lanshare.git → foo/lanshare
  repo=$(git remote get-url origin 2>/dev/null | sed -E 's#(git@|https://)github.com[:/]##; s#\.git$##')
  if [ -z "$repo" ]; then
    echo "没装 Go，也找不到 GitHub 仓库地址。请先安装 Go：https://go.dev/dl/" >&2
    exit 1
  fi
  url="https://github.com/$repo/releases/latest/download/lanshare-$os-$arch"
  echo "下载 $url"
  curl -fL --progress-bar -o "$BIN" "$url"
  chmod +x "$BIN"
  [ "$os" = "darwin" ] && xattr -d com.apple.quarantine "$BIN" 2>/dev/null || true
fi

exec "$BIN" "$@"
