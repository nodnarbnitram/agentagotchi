#pragma once

#include <stdint.h>
#include <stdbool.h>

#include "esp_err.h"
#include "app_settings.h"

esp_err_t app_network_start(const app_settings_t *settings);
esp_err_t app_network_request_focus(const char *task_id, uint64_t seen_seq);
bool app_network_wall_clock_valid(void);
