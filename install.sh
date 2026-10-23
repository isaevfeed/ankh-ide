#!/bin/sh
# Ankh installer: downloads the latest release and puts Ankh.app into
# /Applications (or ~/Applications when /Applications is not writable).
#
#   curl -fsSL https://raw.githubusercontent.com/isaevfeed/ankh-ide/main/install.sh | sh
#
# A specific version:  ... | ANKH_VERSION=v0.1.0 sh
#
# Files downloaded with curl are not marked as "downloaded from the internet",
# so Gatekeeper does not block the app, which is signed ad hoc only.
set -eu

REPO="isaevfeed/ankh-ide"
ASSET="Ankh-macos-universal.zip"
VERSION="${ANKH_VERSION:-latest}"

say() { printf '%s\n' "$*"; }
die() { printf 'ankh: %s\n' "$*" >&2; exit 1; }

[ "$(uname -s)" = "Darwin" ] || die "Ankh работает только на macOS."
major=$(sw_vers -productVersion | cut -d. -f1)
[ "$major" -ge 11 ] || die "нужна macOS 11 или новее."

if [ "$VERSION" = "latest" ]; then
  URL="https://github.com/$REPO/releases/latest/download/$ASSET"
else
  URL="https://github.com/$REPO/releases/download/$VERSION/$ASSET"
fi

TMP=$(mktemp -d)
trap 'rm -rf "$TMP"' EXIT

say "→ Скачиваю $URL"
curl -fL --progress-bar -o "$TMP/$ASSET" "$URL" || die "не удалось скачать релиз."
if curl -fsL -o "$TMP/$ASSET.sha256" "$URL.sha256"; then
  want=$(cut -d' ' -f1 < "$TMP/$ASSET.sha256")
  have=$(shasum -a 256 "$TMP/$ASSET" | cut -d' ' -f1)
  [ "$want" = "$have" ] || die "контрольная сумма не совпала."
  say "→ Контрольная сумма совпала"
fi

ditto -x -k "$TMP/$ASSET" "$TMP/out"
[ -d "$TMP/out/Ankh.app" ] || die "в архиве нет Ankh.app."

DEST="/Applications"
if [ ! -w "$DEST" ]; then
  DEST="$HOME/Applications"
  mkdir -p "$DEST"
fi

if pgrep -x ankh >/dev/null 2>&1; then
  say "→ Закрываю запущенный Ankh"
  osascript -e 'quit app "Ankh"' >/dev/null 2>&1 || true
  sleep 1
fi

rm -rf "$DEST/Ankh.app"
ditto "$TMP/out/Ankh.app" "$DEST/Ankh.app"
# Just in case the archive came another way (browser): drop the quarantine flag.
xattr -dr com.apple.quarantine "$DEST/Ankh.app" 2>/dev/null || true
touch "$DEST/Ankh.app"
LSREG=/System/Library/Frameworks/CoreServices.framework/Frameworks/LaunchServices.framework/Support/lsregister
[ -x "$LSREG" ] && "$LSREG" -f "$DEST/Ankh.app" >/dev/null 2>&1 || true

say "✓ Ankh установлен в $DEST/Ankh.app"
say "  Запуск: open \"$DEST/Ankh.app\""
