#!/bin/sh
# Build the Agentagotchi Edge admin app (clients/macos-admin) into a
# self-contained .app bundle. Works with Xcode Command Line Tools alone
# (no full Xcode required): assembles a minimal bundle around the
# SwiftPM-built executable.
#
# Usage: scripts/build-macos-admin-app.sh [output_dir]
#   output_dir   where the .app is written (default: ./work/macos-admin)
#                The bundle is written as $output_dir/AgentagotchiAdmin.app.
#
# The resulting bundle is unsigned. For a personal tool built and run
# locally this launches normally; for distribution you would need to
# Xcode-sign it (and notarize) instead.

set -eu

project_dir=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
pkg_dir="$project_dir/clients/macos-admin"
out_dir="${1:-$project_dir/work/macos-admin}"

app_name="AgentagotchiAdmin"
bundle_id="com.agentagotchi.admin"

if ! command -v swift >/dev/null 2>&1; then
  echo "error: swift toolchain not found; install Xcode or Command Line Tools" >&2
  exit 1
fi

echo "Building $app_name..."
tmp_build=$(mktemp -d "${TMPDIR:-/tmp}/agentagotchi-admin-build.XXXXXX")
trap 'rm -rf "$tmp_build"' EXIT INT TERM

cd "$pkg_dir"
swift build -c release \
  --scratch-path "$tmp_build/.build" \
  --package-path "$pkg_dir" >/dev/null

exe="$tmp_build/.build/arm64-apple-macosx/release/$app_name"
if [ ! -x "$exe" ]; then
  echo "error: built executable not found at $exe" >&2
  exit 1
fi

# Assemble the minimal .app bundle.
app_dir="$out_dir/$app_name.app"
rm -rf "$app_dir"
mkdir -p "$app_dir/Contents/MacOS"

install -m 0755 "$exe" "$app_dir/Contents/MacOS/$app_name"

cat > "$app_dir/Contents/Info.plist" <<PLIST
<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
	<key>CFBundleExecutable</key>
	<string>$app_name</string>
	<key>CFBundleIdentifier</key>
	<string>$bundle_id</string>
	<key>CFBundleName</key>
	<string>$app_name</string>
	<key>CFBundleDisplayName</key>
	<string>Agentagotchi Edge</string>
	<key>CFBundlePackageType</key>
	<string>APPL</string>
	<key>CFBundleShortVersionString</key>
	<string>0.1.0</string>
	<key>CFBundleVersion</key>
	<string>1</string>
	<key>LSMinimumSystemVersion</key>
	<string>14.0</string>
	<key>NSHighResolutionCapable</key>
	<true/>
	<key>NSPrincipalClass</key>
	<string>NSApplication</string>
	<key>LSUIElement</key>
	<false/>
</dict>
</plist>
PLIST

printf 'APPL????' > "$app_dir/Contents/PkgInfo"

echo "Wrote $app_dir"
