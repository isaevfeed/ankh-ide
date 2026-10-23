#!/bin/bash
# Builds build/bin/Ankh.app: production binary, Info.plist, icon, ad-hoc signature.
set -euo pipefail
cd "$(dirname "$0")/.."

VERSION="${VERSION:-0.1.0}"
APP=build/bin/Ankh.app
rm -rf "$APP"
mkdir -p "$APP/Contents/MacOS" "$APP/Contents/Resources"

if [ ! -f frontend/dist/index.html ]; then
  echo "frontend/dist is empty: run 'make frontend' first" >&2
  exit 1
fi

LDFLAGS="-s -w"
# ARCH=universal builds arm64 and amd64 and joins them with lipo.
if [ "${ARCH:-}" = "universal" ]; then
  echo "→ go build (production, arm64 + amd64)"
  TMP=$(mktemp -d)
  GOARCH=arm64 go build -tags production -trimpath -ldflags "$LDFLAGS" -o "$TMP/ankh-arm64" .
  GOARCH=amd64 go build -tags production -trimpath -ldflags "$LDFLAGS" -o "$TMP/ankh-amd64" .
  lipo -create -output "$APP/Contents/MacOS/ankh" "$TMP/ankh-arm64" "$TMP/ankh-amd64"
else
  echo "→ go build (production)"
  go build -tags production -trimpath -ldflags "$LDFLAGS" -o "$APP/Contents/MacOS/ankh" .
fi

sed "s/{{VERSION}}/$VERSION/g" build/darwin/Info.plist > "$APP/Contents/Info.plist"

if [ -f build/appicon.png ] && command -v iconutil >/dev/null; then
  echo "→ icon"
  ICONSET=$(mktemp -d)/Ankh.iconset
  mkdir -p "$ICONSET"
  for s in 16 32 128 256 512; do
    sips -z $s $s build/appicon.png --out "$ICONSET/icon_${s}x${s}.png" >/dev/null
    sips -z $((s * 2)) $((s * 2)) build/appicon.png --out "$ICONSET/icon_${s}x${s}@2x.png" >/dev/null
  done
  iconutil -c icns "$ICONSET" -o "$APP/Contents/Resources/Ankh.icns"
fi

echo "→ ad-hoc codesign"
codesign --force --deep --sign - "$APP" >/dev/null 2>&1 || echo "  (codesign skipped)"

# Finder and the Dock cache icons by bundle path: make them re-read it.
touch "$APP"
LSREG=/System/Library/Frameworks/CoreServices.framework/Frameworks/LaunchServices.framework/Support/lsregister
[ -x "$LSREG" ] && "$LSREG" -f "$APP" >/dev/null 2>&1 || true

echo "✓ $APP"
