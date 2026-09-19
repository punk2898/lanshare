#!/bin/bash
# 打包 Mac 客户端：dist/局域网共享.app（同时支持 Apple 芯片和 Intel），外加一个方便分发的 zip。
# 需要：Go、Xcode 命令行工具（lipo / iconutil / codesign，Mac 上一般都有）。
set -euo pipefail
cd "$(dirname "$0")/.."

NAME="局域网共享"
VERSION="${VERSION:-1.0.0}"
APP="dist/$NAME.app"
TMP="$(mktemp -d)"
trap 'rm -rf "$TMP"' EXIT

rm -rf "$APP" "dist/$NAME-mac.zip"
mkdir -p "$APP/Contents/MacOS" "$APP/Contents/Resources"

echo "→ 编译（arm64 + amd64）"
for arch in arm64 amd64; do
  GOOS=darwin GOARCH=$arch CGO_ENABLED=0 go build -trimpath -ldflags "-s -w" -o "$TMP/lanshare-$arch" ./cmd/lanshare
done
lipo -create -output "$APP/Contents/MacOS/lanshare" "$TMP/lanshare-arm64" "$TMP/lanshare-amd64"

echo "→ 生成图标"
go run ./scripts/icon "$TMP/icon.png"
ICONSET="$TMP/AppIcon.iconset"
mkdir -p "$ICONSET"
for s in 16 32 128 256 512; do
  sips -z $s $s "$TMP/icon.png" --out "$ICONSET/icon_${s}x${s}.png" >/dev/null
  sips -z $((s*2)) $((s*2)) "$TMP/icon.png" --out "$ICONSET/icon_${s}x${s}@2x.png" >/dev/null
done
iconutil -c icns "$ICONSET" -o "$APP/Contents/Resources/AppIcon.icns"

# LSUIElement：不在 Dock 里占位置，双击后直接在浏览器打开页面，页面上点「停止共享」退出。
cat > "$APP/Contents/Info.plist" <<EOF
<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
  <key>CFBundleName</key><string>$NAME</string>
  <key>CFBundleDisplayName</key><string>$NAME</string>
  <key>CFBundleIdentifier</key><string>app.lanshare</string>
  <key>CFBundleExecutable</key><string>lanshare</string>
  <key>CFBundleIconFile</key><string>AppIcon</string>
  <key>CFBundlePackageType</key><string>APPL</string>
  <key>CFBundleShortVersionString</key><string>$VERSION</string>
  <key>CFBundleVersion</key><string>$VERSION</string>
  <key>LSMinimumSystemVersion</key><string>11.0</string>
  <key>LSUIElement</key><true/>
</dict>
</plist>
EOF

echo "→ 签名（ad-hoc）并打包"
codesign --force --deep --sign - "$APP"
(cd dist && ditto -c -k --keepParent "$NAME.app" "$NAME-mac.zip")

echo "完成：$APP"
echo "      dist/$NAME-mac.zip"
