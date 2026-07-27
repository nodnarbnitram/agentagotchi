#pragma once

#include <stddef.h>

#include "esp_err.h"
#include "app_state.h"

esp_err_t app_protocol_parse_snapshot(const char *json, size_t length, app_snapshot_t *snapshot);
const char *app_protocol_state_name(app_agent_state_t state);
