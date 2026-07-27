#pragma once

#include <stdbool.h>
#include <stdint.h>

#include "freertos/FreeRTOS.h"
#include "freertos/queue.h"

#define APP_MAX_TASKS 12
#define APP_TASK_ID_MAX 48
#define APP_TASK_TITLE_MAX 97
#define APP_REASON_MAX 16

typedef enum {
    APP_STATE_IDLE = 0,
    APP_STATE_RUNNING,
    APP_STATE_NEEDS_INPUT,
    APP_STATE_READY,
    APP_STATE_BLOCKED,
} app_agent_state_t;

typedef struct {
    char id[APP_TASK_ID_MAX];
    char title[APP_TASK_TITLE_MAX];
    app_agent_state_t state;
    char reason[APP_REASON_MAX];
    int subagent_count;
} app_task_t;

typedef struct {
    bool temperature_valid;
    float temperature_c;
    bool humidity_valid;
    float humidity_rh;
    bool battery_valid;
    float battery_voltage;
    int battery_percent;
    bool battery_estimate;
    bool presence;
    bool wifi_connected;
    int wifi_rssi;
    /* Unix seconds, or zero until SNTP has established wall-clock time. */
    int64_t sensor_updated_at;
    /* Monotonic time used only for local stale-value calculations. */
    int64_t sensor_updated_us;
} app_sensor_state_t;

typedef struct {
    uint64_t seq;
    app_agent_state_t aggregate_state;
    int task_count;
    app_task_t tasks[APP_MAX_TASKS];
    int needs_input_count;
    int blocked_count;
    int ready_count;
    int running_count;
    /* Local-only device metrics merged by the LVGL owner task. */
    app_sensor_state_t device;
} app_snapshot_t;

typedef enum {
    APP_UI_EVENT_SNAPSHOT = 0,
    APP_UI_EVENT_SENSOR,
    APP_UI_EVENT_NETWORK,
    APP_UI_EVENT_PROVISIONING,
} app_ui_event_type_t;

typedef struct {
    app_ui_event_type_t type;
    union {
        app_snapshot_t snapshot;
        app_sensor_state_t sensor;
        struct {
            bool websocket_connected;
            bool wifi_connected;
            int rssi;
        } network;
        struct {
            bool waiting;
            char temp_unit;
        } provisioning;
    } data;
} app_ui_event_t;

QueueHandle_t app_ui_queue(void);
void app_ui_post(const app_ui_event_t *event);
