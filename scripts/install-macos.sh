#!/bin/sh
set -eu

script_dir=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
project_dir=$(CDPATH= cd -- "$script_dir/.." && pwd)
app_dir="$HOME/Library/Application Support/Agentagotchi"
bin_dir="$app_dir/bin"
plugin_dir="$HOME/plugins/agentagotchi-status"
launch_dir="$HOME/Library/LaunchAgents"
launch_plist="$launch_dir/com.agentagotchi.edge.plist"
codex_bin="${AGENTAGOTCHI_CODEX_BIN:-}"
build_dir=$(mktemp -d "${TMPDIR:-/tmp}/agentagotchi-install.XXXXXX")

cleanup() {
  rm -rf "$build_dir"
}
trap cleanup EXIT INT TERM

if [ -z "$codex_bin" ]; then
  for candidate in \
    "/Applications/ChatGPT.app/Contents/Resources/codex" \
    "/Applications/Codex.app/Contents/Resources/codex"
  do
    if [ -x "$candidate" ]; then
      codex_bin="$candidate"
      break
    fi
  done
fi
if [ -z "$codex_bin" ] || [ ! -x "$codex_bin" ]; then
  echo "Codex CLI not found; install the Codex desktop app or set AGENTAGOTCHI_CODEX_BIN." >&2
  exit 1
fi

cd "$project_dir"
GOCACHE="$build_dir/gocache" go test ./...
GOCACHE="$build_dir/gocache" GOOS=darwin GOARCH=arm64 \
  go build -trimpath -o "$build_dir/agentagotchi" ./cmd/agentagotchi

mkdir -p "$bin_dir" "$plugin_dir" "$launch_dir"
chmod 0700 "$app_dir" "$bin_dir"
install -m 0755 "$build_dir/agentagotchi" "$bin_dir/agentagotchi"
ditto "$project_dir/plugin/agentagotchi-status" "$plugin_dir"
sed -e "s|__APP_DIR__|$app_dir|g" -e "s|__CODEX_BIN__|$codex_bin|g" \
  "$project_dir/packaging/com.agentagotchi.edge.plist.in" > "$launch_plist"
chmod 0644 "$launch_plist"

launchctl bootout "gui/$(id -u)/com.agentagotchi.edge" >/dev/null 2>&1 || true
launchctl bootstrap "gui/$(id -u)" "$launch_plist"
launchctl kickstart -k "gui/$(id -u)/com.agentagotchi.edge"

if ! "$codex_bin" plugin add agentagotchi-status@personal; then
  echo "The bridge is installed, but Codex did not enable agentagotchi-status@personal." >&2
  exit 1
fi

echo "Agentagotchi installed. Restart Codex so the hook plugin is reloaded."
echo "Bridge log: $app_dir/bridge.log"
