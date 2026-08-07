#!/bin/sh
set -eu

script_dir=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
project_dir=$(CDPATH= cd -- "$script_dir/.." && pwd)
app_dir="$HOME/Library/Application Support/Agentagotchi"
bin_dir="$app_dir/bin"
# Codex's personal marketplace resolves plugins from ~/.codex/personal/plugins/
# and enumerates them via .agents/plugins/marketplace.json; both must be updated.
plugin_dir="$HOME/.codex/personal/plugins/agentagotchi-status"
marketplace_index="$HOME/.codex/personal/.agents/plugins/marketplace.json"
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

mkdir -p "$bin_dir" "$plugin_dir" "$launch_dir" "$app_dir"
chmod 0700 "$app_dir" "$bin_dir"
install -m 0755 "$build_dir/agentagotchi" "$bin_dir/agentagotchi"
ditto "$project_dir/plugin/agentagotchi-status" "$plugin_dir"

# Build and install the optional macOS admin client (.app bundle).
# The bundle is unsigned; usable as a local owner tool. For distribution
# you would Xcode-sign and notarize it instead.
if command -v swift >/dev/null 2>&1; then
  echo "Building macos-admin app..."
  admin_bundle="$build_dir/macos-admin"
  "$project_dir/scripts/build-macos-admin-app.sh" "$admin_bundle" >/dev/null
  install_dir="$app_dir/AgentagotchiAdmin.app"
  rm -rf "$install_dir"
  ditto "$admin_bundle/AgentagotchiAdmin.app" "$install_dir"
  chmod -R u+rwX,go-rwx "$install_dir"
  echo "Admin app installed: $install_dir"
else
  echo "warning: swift toolchain not found; skipping the optional macos-admin app" >&2
fi

# Register the plugin in the personal marketplace index (idempotent).
if [ -f "$marketplace_index" ]; then
  python3 - "$marketplace_index" <<'PY'
import json, sys
path = sys.argv[1]
data = json.load(open(path))
plugins = data.setdefault("plugins", [])
if not any(p.get("name") == "agentagotchi-status" for p in plugins):
    plugins.append({
        "name": "agentagotchi-status",
        "source": {"source": "local", "path": "./plugins/agentagotchi-status"},
        "category": "Productivity",
    })
    json.dump(data, open(path, "w"), indent=2)
PY
else
  echo "warning: $marketplace_index not found; plugin may not be discoverable" >&2
fi
sed -e "s|__APP_DIR__|$app_dir|g" -e "s|__CODEX_BIN__|$codex_bin|g" \
  "$project_dir/packaging/com.agentagotchi.edge.plist.in" > "$launch_plist"
chmod 0644 "$launch_plist"

launchctl bootout "gui/$(id -u)/com.agentagotchi.edge" >/dev/null 2>&1 || true
launchctl bootstrap "gui/$(id -u)" "$launch_plist"
launchctl kickstart -k "gui/$(id -u)/com.agentagotchi.edge"

# The superseded single-harness prototype plugin, if present and enabled, fires
# hooks at the retired codex-pet bridge. Disable it as part of migration.
if "$codex_bin" plugin list 2>/dev/null | grep -q "codex-pet-status@personal"; then
  "$codex_bin" plugin remove codex-pet-status@personal >/dev/null 2>&1 || true
fi

if ! "$codex_bin" plugin add agentagotchi-status@personal; then
  echo "The bridge is installed, but Codex did not enable agentagotchi-status@personal." >&2
  exit 1
fi

echo "Agentagotchi installed. Restart Codex so the hook plugin is reloaded."
echo "Bridge log: $app_dir/bridge.log"
echo "Admin app: $app_dir/AgentagotchiAdmin.app (open to launch)"
