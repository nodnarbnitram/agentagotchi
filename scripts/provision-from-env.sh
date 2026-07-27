#!/bin/sh
set -eu

script_dir=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
project_dir=$(CDPATH= cd -- "$script_dir/.." && pwd)
env_file=${CODEX_PET_ENV_FILE:-"$project_dir/.env"}

read_env_value() {
  key=$1
  value=$(
    awk -v wanted="$key" \
      'index($0, wanted "=") == 1 { sub(/^[^=]*=/, ""); print; exit }' \
      "$env_file"
  )
  case "$value" in
    \"*\") value=${value#\"}; value=${value%\"} ;;
    \'*\') value=${value#\'}; value=${value%\'} ;;
  esac
  printf '%s' "$value"
}

if [ ! -f "$env_file" ]; then
  echo "Missing $env_file; add WIFI_SSID and WIFI_PASSWORD." >&2
  exit 1
fi

wifi_ssid=$(read_env_value WIFI_SSID)
wifi_password=$(read_env_value WIFI_PASSWORD)
temp_unit=${CODEX_PET_TEMP_UNIT:-F}
serial_port=${CODEX_PET_SERIAL:-}

if [ -z "$wifi_ssid" ] || [ -z "$wifi_password" ]; then
  echo "WIFI_SSID and WIFI_PASSWORD must both be set in $env_file." >&2
  exit 1
fi

if [ -x "$project_dir/work/bin/codex-pet" ]; then
  codex_pet="$project_dir/work/bin/codex-pet"
elif [ -x "$HOME/Library/Application Support/CodexPet/bin/codex-pet" ]; then
  codex_pet="$HOME/Library/Application Support/CodexPet/bin/codex-pet"
else
  echo "Codex Pet binary not found; run 'make build-host' first." >&2
  exit 1
fi

set -- provision --skip-flash --password-stdin \
  --ssid "$wifi_ssid" --temp-unit "$temp_unit"
if [ -n "$serial_port" ]; then
  set -- "$@" --serial "$serial_port"
fi

printf '%s' "$wifi_password" | "$codex_pet" "$@"
