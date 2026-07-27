#!/bin/sh
set -eu

script_dir=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
project_dir=$(CDPATH= cd -- "$script_dir/.." && pwd)
app_dir="$HOME/Library/Application Support/CodexPet"
bin_dir="$app_dir/bin"
plugin_dir="$HOME/plugins/codex-pet-status"
launch_dir="$HOME/Library/LaunchAgents"
launch_plist="$launch_dir/com.openai.codexpet.plist"
codex_bin="${CODEX_PET_CODEX_BIN:-}"
build_dir=$(mktemp -d "${TMPDIR:-/tmp}/codex-pet-install.XXXXXX")

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
  echo "Codex CLI not found; install the Codex desktop app or set CODEX_PET_CODEX_BIN." >&2
  exit 1
fi

cd "$project_dir"
GOCACHE="$build_dir/gocache" go test ./...
GOCACHE="$build_dir/gocache" GOOS=darwin GOARCH=arm64 \
  go build -trimpath -o "$build_dir/codex-pet" ./cmd/codex-pet

mkdir -p "$bin_dir" "$plugin_dir" "$launch_dir"
chmod 0700 "$app_dir" "$bin_dir"
install -m 0755 "$build_dir/codex-pet" "$bin_dir/codex-pet"
ditto "$project_dir/plugin/codex-pet-status" "$plugin_dir"
sed -e "s|__APP_DIR__|$app_dir|g" -e "s|__CODEX_BIN__|$codex_bin|g" \
  "$project_dir/packaging/com.openai.codexpet.plist.in" > "$launch_plist"
chmod 0644 "$launch_plist"

launchctl bootout "gui/$(id -u)/com.openai.codexpet" >/dev/null 2>&1 || true
launchctl bootstrap "gui/$(id -u)" "$launch_plist"
launchctl kickstart -k "gui/$(id -u)/com.openai.codexpet"

if ! "$codex_bin" plugin add codex-pet-status@personal; then
  echo "The bridge is installed, but Codex did not enable codex-pet-status@personal." >&2
  exit 1
fi

echo "Codex Pet installed. Restart Codex so the hook plugin is reloaded."
echo "Bridge log: $app_dir/bridge.log"
