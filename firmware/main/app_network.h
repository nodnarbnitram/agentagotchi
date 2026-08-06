#pragma once

#include <stdint.h>
#include <stdbool.h>

#include "esp_err.h"
#include "app_settings.h"
#include "app_state.h"

typedef enum {
    APP_DISMISS_ACKNOWLEDGE = 0,
    APP_DISMISS_SNOOZE,
} app_dismiss_mode_t;

esp_err_t app_network_start(const app_settings_t *settings);
esp_err_t app_network_request_focus(
    const char *task_id,
    int feed_slot,
    uint64_t seen_revision);
esp_err_t app_network_request_dismiss(
    const char *task_id,
    int feed_slot,
    uint64_t seen_revision,
    app_dismiss_mode_t mode);
bool app_network_wall_clock_valid(void);
