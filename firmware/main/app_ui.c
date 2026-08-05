#include "app_ui.h"

#include <math.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>

#include "app_audio.h"
#include "app_font_symbols.h"
#include "app_network.h"
#include "app_protocol.h"
#include "app_state.h"
#include "bsp/esp-bsp.h"
#include "esp_check.h"
#include "esp_heap_caps.h"
#include "esp_log.h"
#include "freertos/FreeRTOS.h"
#include "freertos/queue.h"
#include "freertos/task.h"
#include "lvgl.h"
#include "pet_asset.h"
#include "sensor_math.h"

#define STATUS_BAR_HEIGHT 20
#define PET_WIDTH 192
#define PET_HEIGHT 144
#define UI_TIMER_MS 100
#define STATUS_REDRAW_MS 1000
#define TRAY_TASKS_VISIBLE 7
#define SCREEN_WIDTH 320
#define BUBBLE_TOP 27
#define BUBBLE_RIGHT (SCREEN_WIDTH - 8)
#define BUBBLE_MIN_WIDTH 164
#define BUBBLE_MAX_WIDTH 304
#define BUBBLE_PADDING_X 12
#define BUBBLE_PADDING_TOP 6
#define BUBBLE_PADDING_BOTTOM 7
#define BUBBLE_TITLE_MAX_LINES 3

typedef struct {
    app_settings_t settings;
    lv_font_t status_font;
    app_snapshot_t snapshot;
    app_sensor_state_t sensors;
    pet_asset_t pet_asset;
    bool has_snapshot;
    bool provisioning;
    bool websocket_connected;
    int wifi_rssi;
    uint8_t animation_frame;
    uint16_t *pet_pixels;
    TickType_t last_status_draw;
    bool status_dirty;

    lv_obj_t *status_bar;
    lv_obj_t *wifi_bars[4];
    lv_obj_t *battery_body;
    lv_obj_t *battery_fill;
    lv_obj_t *battery_label;
    lv_obj_t *temperature_label;
    lv_obj_t *humidity_label;
    lv_obj_t *presence_dot;
    lv_obj_t *presence_label;
    lv_obj_t *pet_canvas;
    lv_obj_t *speech_tail;
    lv_obj_t *speech_bubble;
    lv_obj_t *state_label;
    lv_obj_t *title_label;
    lv_obj_t *reason_label;
    lv_obj_t *counts_label;
    lv_obj_t *subagents_label;
    lv_obj_t *task_tray;
    lv_obj_t *tray_labels[TRAY_TASKS_VISIBLE];

    int rendered_wifi_bars;
    int rendered_battery_percent;
    bool rendered_battery_valid;
    bool rendered_presence;
    bool rendered_presence_valid;
    char rendered_battery[16];
    char rendered_temperature[16];
    char rendered_humidity[16];
} ui_context_t;

static const char *TAG = "pet_ui";
static ui_context_t s_ui;

static lv_color_t state_color(app_agent_state_t state)
{
    switch (state) {
    case APP_STATE_NEEDS_INPUT:
        return lv_color_hex(0xFFB84D);
    case APP_STATE_BLOCKED:
        return lv_color_hex(0xFF6B6B);
    case APP_STATE_READY:
        return lv_color_hex(0x73E2A7);
    case APP_STATE_RUNNING:
        return lv_color_hex(0x62D7F5);
    default:
        return lv_color_hex(0xA8B6C1);
    }
}

static const char *reason_text(const char *reason)
{
    if (strcmp(reason, "question") == 0) {
        return "Has a question";
    }
    if (strcmp(reason, "permission") == 0) {
        return "Permission requested";
    }
    if (strcmp(reason, "approval") == 0) {
        return "Approval needed";
    }
    if (strcmp(reason, "failed") == 0) {
        return "Needs attention";
    }
    if (strcmp(reason, "completed") == 0) {
        return "Finished";
    }
    if (strcmp(reason, "working") == 0) {
        return "Working";
    }
    return "Codex is quiet";
}

static lv_obj_t *make_label(
    lv_obj_t *parent,
    const lv_font_t *font,
    lv_color_t color,
    lv_text_align_t alignment)
{
    lv_obj_t *label = lv_label_create(parent);
    lv_obj_set_style_text_font(label, font, 0);
    lv_obj_set_style_text_color(label, color, 0);
    lv_obj_set_style_text_align(label, alignment, 0);
    return label;
}

static int32_t clamp_dimension(int32_t value, int32_t minimum, int32_t maximum)
{
    if (value < minimum) {
        return minimum;
    }
    if (value > maximum) {
        return maximum;
    }
    return value;
}

static void measure_text(
    lv_point_t *size,
    const char *text,
    const lv_font_t *font,
    int32_t max_width)
{
    lv_text_get_size(
        size,
        text,
        font,
        0,
        0,
        max_width,
        LV_TEXT_FLAG_NONE);
}

static void layout_speech_bubble(void)
{
    const char *state = lv_label_get_text(s_ui.state_label);
    const char *title = lv_label_get_text(s_ui.title_label);
    const char *reason = lv_label_get_text(s_ui.reason_label);
    lv_point_t state_size;
    lv_point_t title_size;
    lv_point_t reason_size;

    measure_text(&state_size, state, &lv_font_montserrat_12, LV_COORD_MAX);
    measure_text(&title_size, title, &lv_font_montserrat_14, LV_COORD_MAX);
    measure_text(&reason_size, reason, &lv_font_montserrat_12, LV_COORD_MAX);

    int32_t natural_width = LV_MAX(
        state_size.x,
        LV_MAX(title_size.x, reason_size.x));
    int32_t bubble_width = clamp_dimension(
        natural_width + (BUBBLE_PADDING_X * 2),
        BUBBLE_MIN_WIDTH,
        BUBBLE_MAX_WIDTH);
    int32_t bubble_x = BUBBLE_RIGHT - bubble_width;
    int32_t content_width = bubble_width - (BUBBLE_PADDING_X * 2);

    measure_text(&title_size, title, &lv_font_montserrat_14, content_width);
    int32_t title_line_height = lv_font_get_line_height(&lv_font_montserrat_14);
    int32_t title_height = LV_MIN(
        title_size.y,
        title_line_height * BUBBLE_TITLE_MAX_LINES);

    int32_t state_y = BUBBLE_TOP + BUBBLE_PADDING_TOP;
    int32_t title_y = state_y + state_size.y + 1;
    int32_t reason_y = title_y + title_height + 3;
    int32_t bubble_height =
        reason_y + reason_size.y + BUBBLE_PADDING_BOTTOM - BUBBLE_TOP;

    lv_obj_set_pos(s_ui.speech_bubble, bubble_x, BUBBLE_TOP);
    lv_obj_set_size(s_ui.speech_bubble, bubble_width, bubble_height);

    int32_t text_x = bubble_x + BUBBLE_PADDING_X;
    lv_obj_set_pos(s_ui.state_label, text_x, state_y);
    lv_obj_set_width(s_ui.state_label, content_width);
    lv_obj_set_pos(s_ui.title_label, text_x, title_y);
    lv_obj_set_size(s_ui.title_label, content_width, title_height);
    lv_obj_set_pos(s_ui.reason_label, text_x, reason_y);
    lv_obj_set_size(s_ui.reason_label, content_width, reason_size.y);

    int32_t tail_x = clamp_dimension(bubble_x + 18, 112, 140);
    lv_obj_set_pos(
        s_ui.speech_tail,
        tail_x,
        BUBBLE_TOP + bubble_height - 10);
}

static void update_tray(void)
{
    for (int i = 0; i < TRAY_TASKS_VISIBLE; ++i) {
        if (i < s_ui.snapshot.task_count) {
            app_task_t *task = &s_ui.snapshot.tasks[i];
            lv_label_set_text_fmt(
                s_ui.tray_labels[i],
                "%s  %s",
                app_protocol_state_name(task->state),
                task->title[0] == '\0' ? "Codex task" : task->title);
            lv_obj_set_style_text_color(s_ui.tray_labels[i], state_color(task->state), 0);
        } else {
            lv_label_set_text(s_ui.tray_labels[i], "");
        }
    }
}

static bool task_was_needing_input(const app_snapshot_t *old_snapshot, const char *task_id)
{
    for (int i = 0; i < old_snapshot->task_count; ++i) {
        if (strcmp(old_snapshot->tasks[i].id, task_id) == 0) {
            return old_snapshot->tasks[i].state == APP_STATE_NEEDS_INPUT;
        }
    }
    return false;
}

static bool contains_new_input_transition(const app_snapshot_t *next)
{
    if (s_ui.has_snapshot && next->seq == s_ui.snapshot.seq) {
        return false;
    }
    for (int i = 0; i < next->task_count; ++i) {
        if (next->tasks[i].state == APP_STATE_NEEDS_INPUT &&
            (!s_ui.has_snapshot ||
                !task_was_needing_input(&s_ui.snapshot, next->tasks[i].id))) {
            return true;
        }
    }
    return false;
}

static void render_main_content(void)
{
    if (s_ui.provisioning) {
        lv_label_set_text(s_ui.state_label, "SETUP");
        lv_obj_set_style_text_color(s_ui.state_label, lv_color_hex(0x62D7F5), 0);
        lv_label_set_text(s_ui.title_label, "Waiting for USB");
        lv_label_set_text(s_ui.reason_label, "Provision me from the Mac");
        lv_label_set_text(s_ui.counts_label, "NO TASKS");
        lv_label_set_text(s_ui.subagents_label, "");
        layout_speech_bubble();
        return;
    }

    app_agent_state_t state = s_ui.has_snapshot
        ? s_ui.snapshot.aggregate_state
        : APP_STATE_IDLE;
    lv_label_set_text(s_ui.state_label, app_protocol_state_name(state));
    lv_obj_set_style_text_color(s_ui.state_label, state_color(state), 0);
    if (!s_ui.has_snapshot || s_ui.snapshot.task_count == 0) {
        lv_label_set_text(s_ui.title_label, "All quiet");
        lv_label_set_text(
            s_ui.reason_label,
            s_ui.websocket_connected ? "Waiting for Codex" : "Mac bridge offline");
    } else {
        const app_task_t *task = &s_ui.snapshot.tasks[0];
        lv_label_set_text(
            s_ui.title_label,
            task->title[0] == '\0' ? "Codex task" : task->title);
        lv_label_set_text(s_ui.reason_label, reason_text(task->reason));
    }
    lv_label_set_text_fmt(
        s_ui.counts_label,
        "Q%d  B%d  R%d  W%d",
        s_ui.snapshot.needs_input_count,
        s_ui.snapshot.blocked_count,
        s_ui.snapshot.ready_count,
        s_ui.snapshot.running_count);
    int subagent_total = 0;
    for (int i = 0; i < s_ui.snapshot.task_count; ++i) {
        subagent_total += s_ui.snapshot.tasks[i].subagent_count;
    }
    lv_label_set_text_fmt(
        s_ui.subagents_label,
        "%d task%s | %d agent%s",
        s_ui.snapshot.task_count,
        s_ui.snapshot.task_count == 1 ? "" : "s",
        subagent_total,
        subagent_total == 1 ? "" : "s");
    layout_speech_bubble();
    update_tray();
}

static void apply_snapshot(const app_snapshot_t *snapshot)
{
    bool chirp = contains_new_input_transition(snapshot);
    s_ui.snapshot = *snapshot;
    s_ui.snapshot.device = s_ui.sensors;
    s_ui.has_snapshot = true;
    s_ui.provisioning = false;
    render_main_content();
    if (chirp) {
        app_audio_request_chirp();
    }
}

#if CONFIG_AGENTAGOTCHI_SENSOR_BAR
static void set_label_if_changed(lv_obj_t *label, char *cached, size_t capacity, const char *value)
{
    if (strncmp(cached, value, capacity) == 0) {
        return;
    }
    snprintf(cached, capacity, "%s", value);
    lv_label_set_text(label, value);
}

static int wifi_bar_count(void)
{
    if (!s_ui.sensors.wifi_connected) {
        return 0;
    }
    if (s_ui.wifi_rssi >= -58) {
        return 4;
    }
    if (s_ui.wifi_rssi >= -68) {
        return 3;
    }
    if (s_ui.wifi_rssi >= -78) {
        return 2;
    }
    return 1;
}
#endif

static void update_status_bar(void)
{
#if CONFIG_AGENTAGOTCHI_SENSOR_BAR
    int bars = wifi_bar_count();
    if (bars != s_ui.rendered_wifi_bars) {
        for (int i = 0; i < 4; ++i) {
            lv_obj_set_style_bg_color(
                s_ui.wifi_bars[i],
                i < bars ? lv_color_hex(0x62D7F5) : lv_color_hex(0x31414A),
                0);
        }
        s_ui.rendered_wifi_bars = bars;
    }

    char text[16];
    if (s_ui.sensors.battery_valid) {
        snprintf(text, sizeof(text), "%d%%", s_ui.sensors.battery_percent);
    } else {
        snprintf(text, sizeof(text), "—");
    }
    set_label_if_changed(
        s_ui.battery_label,
        s_ui.rendered_battery,
        sizeof(s_ui.rendered_battery),
        text);
    if (s_ui.sensors.battery_valid != s_ui.rendered_battery_valid ||
        s_ui.sensors.battery_percent != s_ui.rendered_battery_percent) {
        int percent = s_ui.sensors.battery_valid ? s_ui.sensors.battery_percent : 0;
        int width = (percent * 14) / 100;
        lv_obj_set_width(s_ui.battery_fill, width);
        lv_obj_set_style_bg_color(
            s_ui.battery_fill,
            percent <= 15 ? lv_color_hex(0xFF6B6B) : lv_color_hex(0x73E2A7),
            0);
        s_ui.rendered_battery_percent = percent;
        s_ui.rendered_battery_valid = s_ui.sensors.battery_valid;
    }

    if (s_ui.sensors.temperature_valid) {
        float value = s_ui.settings.temp_unit == 'C'
            ? s_ui.sensors.temperature_c
            : sensor_math_c_to_f(s_ui.sensors.temperature_c);
        snprintf(
            text,
            sizeof(text),
            "%.0f°%c",
            value,
            s_ui.settings.temp_unit == 'C' ? 'C' : 'F');
    } else {
        snprintf(text, sizeof(text), "—°%c", s_ui.settings.temp_unit == 'C' ? 'C' : 'F');
    }
    set_label_if_changed(
        s_ui.temperature_label,
        s_ui.rendered_temperature,
        sizeof(s_ui.rendered_temperature),
        text);

    if (s_ui.sensors.humidity_valid) {
        snprintf(text, sizeof(text), "%.0f%%RH", s_ui.sensors.humidity_rh);
    } else {
        snprintf(text, sizeof(text), "—%%RH");
    }
    set_label_if_changed(
        s_ui.humidity_label,
        s_ui.rendered_humidity,
        sizeof(s_ui.rendered_humidity),
        text);

    if (!s_ui.rendered_presence_valid ||
        s_ui.sensors.presence != s_ui.rendered_presence) {
        lv_obj_set_style_bg_color(
            s_ui.presence_dot,
            s_ui.sensors.presence ? lv_color_hex(0xFFB84D) : lv_color_hex(0x31414A),
            0);
        lv_obj_set_style_text_color(
            s_ui.presence_label,
            s_ui.sensors.presence ? lv_color_hex(0xFFCF78) : lv_color_hex(0x697B87),
            0);
        s_ui.rendered_presence = s_ui.sensors.presence;
        s_ui.rendered_presence_valid = true;
    }
#endif
}

static void pet_click_handler(lv_event_t *event)
{
    if (lv_event_get_code(event) == LV_EVENT_RELEASED &&
        s_ui.has_snapshot && s_ui.snapshot.task_count > 0) {
        (void)app_network_request_focus(
            s_ui.snapshot.tasks[0].id,
            s_ui.snapshot.seq);
    }
}

static void counts_click_handler(lv_event_t *event)
{
    if (lv_event_get_code(event) != LV_EVENT_RELEASED) {
        return;
    }
    if (lv_obj_has_flag(s_ui.task_tray, LV_OBJ_FLAG_HIDDEN)) {
        update_tray();
        lv_obj_remove_flag(s_ui.task_tray, LV_OBJ_FLAG_HIDDEN);
        lv_obj_move_foreground(s_ui.task_tray);
    } else {
        lv_obj_add_flag(s_ui.task_tray, LV_OBJ_FLAG_HIDDEN);
    }
}

static void tray_click_handler(lv_event_t *event)
{
    if (lv_event_get_code(event) == LV_EVENT_RELEASED) {
        lv_obj_add_flag(s_ui.task_tray, LV_OBJ_FLAG_HIDDEN);
    }
}

static void animate_pet(void)
{
    if (s_ui.pet_pixels == NULL || s_ui.pet_canvas == NULL) {
        return;
    }
    app_agent_state_t state = s_ui.has_snapshot
        ? s_ui.snapshot.aggregate_state
        : APP_STATE_IDLE;
    if (pet_asset_copy_frame(
            &s_ui.pet_asset,
            state,
            s_ui.animation_frame,
            s_ui.pet_pixels,
            PET_WIDTH * PET_HEIGHT)) {
        lv_obj_invalidate(s_ui.pet_canvas);
    }
    s_ui.animation_frame++;
}

static void ui_timer_handler(lv_timer_t *timer)
{
    (void)timer;
    /* LVGL invokes this callback serially; static storage avoids putting the
     * full task snapshot union on the display task's stack. */
    static app_ui_event_t event;
    while (xQueueReceive(app_ui_queue(), &event, 0) == pdTRUE) {
        switch (event.type) {
        case APP_UI_EVENT_SNAPSHOT:
            apply_snapshot(&event.data.snapshot);
            break;
        case APP_UI_EVENT_SENSOR:
            event.data.sensor.wifi_connected = s_ui.sensors.wifi_connected;
            event.data.sensor.wifi_rssi = s_ui.sensors.wifi_rssi;
            s_ui.sensors = event.data.sensor;
            if (s_ui.has_snapshot) {
                s_ui.snapshot.device = s_ui.sensors;
            }
            s_ui.status_dirty = true;
            break;
        case APP_UI_EVENT_NETWORK:
            s_ui.websocket_connected = event.data.network.websocket_connected;
            s_ui.sensors.wifi_connected = event.data.network.wifi_connected;
            if (!event.data.network.wifi_connected) {
                s_ui.wifi_rssi = -127;
                s_ui.sensors.wifi_rssi = -127;
            } else if (event.data.network.rssi != -127) {
                s_ui.wifi_rssi = event.data.network.rssi;
                s_ui.sensors.wifi_rssi = event.data.network.rssi;
            }
            if (s_ui.has_snapshot) {
                s_ui.snapshot.device = s_ui.sensors;
            }
            s_ui.status_dirty = true;
            render_main_content();
            break;
        case APP_UI_EVENT_PROVISIONING:
            s_ui.provisioning = event.data.provisioning.waiting;
            if (event.data.provisioning.temp_unit == 'C' ||
                event.data.provisioning.temp_unit == 'F') {
                s_ui.settings.temp_unit = event.data.provisioning.temp_unit;
                s_ui.status_dirty = true;
            }
            render_main_content();
            break;
        }
    }

    animate_pet();
    TickType_t now = xTaskGetTickCount();
    if (s_ui.status_dirty &&
        (s_ui.last_status_draw == 0 ||
            now - s_ui.last_status_draw >= pdMS_TO_TICKS(STATUS_REDRAW_MS))) {
        update_status_bar();
        s_ui.last_status_draw = now;
        s_ui.status_dirty = false;
    }
}

static void build_status_bar(lv_obj_t *screen)
{
#if CONFIG_AGENTAGOTCHI_SENSOR_BAR
    s_ui.status_bar = lv_obj_create(screen);
    lv_obj_set_pos(s_ui.status_bar, 0, 0);
    lv_obj_set_size(s_ui.status_bar, 320, STATUS_BAR_HEIGHT);
    lv_obj_set_style_bg_color(s_ui.status_bar, lv_color_hex(0x132129), 0);
    lv_obj_set_style_bg_opa(s_ui.status_bar, LV_OPA_COVER, 0);
    lv_obj_set_style_border_width(s_ui.status_bar, 0, 0);
    lv_obj_set_style_radius(s_ui.status_bar, 0, 0);
    lv_obj_set_style_pad_all(s_ui.status_bar, 0, 0);
    lv_obj_remove_flag(s_ui.status_bar, LV_OBJ_FLAG_SCROLLABLE);

    for (int i = 0; i < 4; ++i) {
        s_ui.wifi_bars[i] = lv_obj_create(s_ui.status_bar);
        lv_obj_set_pos(s_ui.wifi_bars[i], 4 + i * 5, 14 - i * 3);
        lv_obj_set_size(s_ui.wifi_bars[i], 3, 3 + i * 3);
        lv_obj_set_style_bg_color(s_ui.wifi_bars[i], lv_color_hex(0x31414A), 0);
        lv_obj_set_style_bg_opa(s_ui.wifi_bars[i], LV_OPA_COVER, 0);
        lv_obj_set_style_border_width(s_ui.wifi_bars[i], 0, 0);
        lv_obj_set_style_radius(s_ui.wifi_bars[i], 1, 0);
    }

    s_ui.battery_body = lv_obj_create(s_ui.status_bar);
    lv_obj_set_pos(s_ui.battery_body, 29, 5);
    lv_obj_set_size(s_ui.battery_body, 18, 10);
    lv_obj_set_style_bg_color(s_ui.battery_body, lv_color_hex(0x263943), 0);
    lv_obj_set_style_border_color(s_ui.battery_body, lv_color_hex(0x8497A1), 0);
    lv_obj_set_style_border_width(s_ui.battery_body, 1, 0);
    lv_obj_set_style_radius(s_ui.battery_body, 1, 0);
    lv_obj_set_style_pad_all(s_ui.battery_body, 1, 0);
    lv_obj_remove_flag(s_ui.battery_body, LV_OBJ_FLAG_SCROLLABLE);
    s_ui.battery_fill = lv_obj_create(s_ui.battery_body);
    lv_obj_set_pos(s_ui.battery_fill, 0, 0);
    lv_obj_set_size(s_ui.battery_fill, 0, 6);
    lv_obj_set_style_border_width(s_ui.battery_fill, 0, 0);
    lv_obj_set_style_radius(s_ui.battery_fill, 0, 0);
    s_ui.battery_label = make_label(
        s_ui.status_bar, &s_ui.status_font, lv_color_hex(0xD7E2E7), LV_TEXT_ALIGN_LEFT);
    lv_obj_set_pos(s_ui.battery_label, 51, 2);
    lv_obj_set_width(s_ui.battery_label, 40);

    s_ui.temperature_label = make_label(
        s_ui.status_bar, &s_ui.status_font, lv_color_hex(0xD7E2E7), LV_TEXT_ALIGN_CENTER);
    lv_obj_set_pos(s_ui.temperature_label, 89, 2);
    lv_obj_set_width(s_ui.temperature_label, 54);
    s_ui.humidity_label = make_label(
        s_ui.status_bar, &s_ui.status_font, lv_color_hex(0xD7E2E7), LV_TEXT_ALIGN_CENTER);
    lv_obj_set_pos(s_ui.humidity_label, 142, 2);
    lv_obj_set_width(s_ui.humidity_label, 62);

    s_ui.presence_dot = lv_obj_create(s_ui.status_bar);
    lv_obj_set_pos(s_ui.presence_dot, 218, 6);
    lv_obj_set_size(s_ui.presence_dot, 8, 8);
    lv_obj_set_style_bg_color(s_ui.presence_dot, lv_color_hex(0x31414A), 0);
    lv_obj_set_style_bg_opa(s_ui.presence_dot, LV_OPA_COVER, 0);
    lv_obj_set_style_border_width(s_ui.presence_dot, 0, 0);
    lv_obj_set_style_radius(s_ui.presence_dot, LV_RADIUS_CIRCLE, 0);
    s_ui.presence_label = make_label(
        s_ui.status_bar, &lv_font_montserrat_12, lv_color_hex(0x697B87), LV_TEXT_ALIGN_LEFT);
    lv_label_set_text(s_ui.presence_label, "PRESENCE");
    lv_obj_set_pos(s_ui.presence_label, 231, 2);
#else
    (void)screen;
#endif
}

static void build_ui(void *argument)
{
    (void)argument;
    lv_obj_t *screen = lv_screen_active();
    lv_obj_set_style_bg_color(screen, lv_color_hex(0x0B151B), 0);
    lv_obj_set_style_bg_opa(screen, LV_OPA_COVER, 0);
    lv_obj_remove_flag(screen, LV_OBJ_FLAG_SCROLLABLE);
    s_ui.status_font = lv_font_montserrat_12;
    s_ui.status_font.fallback = &app_font_em_dash_12;
    build_status_bar(screen);

    s_ui.pet_canvas = lv_canvas_create(screen);
    lv_canvas_set_buffer(
        s_ui.pet_canvas,
        s_ui.pet_pixels,
        PET_WIDTH,
        PET_HEIGHT,
        LV_COLOR_FORMAT_RGB565);
    lv_obj_set_pos(s_ui.pet_canvas, 0, 96);
    lv_obj_add_flag(s_ui.pet_canvas, LV_OBJ_FLAG_CLICKABLE);
    lv_obj_add_event_cb(s_ui.pet_canvas, pet_click_handler, LV_EVENT_RELEASED, NULL);

    s_ui.speech_tail = lv_obj_create(screen);
    lv_obj_set_pos(s_ui.speech_tail, 116, 88);
    lv_obj_set_size(s_ui.speech_tail, 18, 22);
    lv_obj_set_style_bg_color(s_ui.speech_tail, lv_color_hex(0xF1F5F7), 0);
    lv_obj_set_style_bg_opa(s_ui.speech_tail, LV_OPA_COVER, 0);
    lv_obj_set_style_border_color(s_ui.speech_tail, lv_color_hex(0xB7C8D0), 0);
    lv_obj_set_style_border_width(s_ui.speech_tail, 1, 0);
    lv_obj_set_style_radius(s_ui.speech_tail, 9, 0);
    lv_obj_set_style_pad_all(s_ui.speech_tail, 0, 0);
    lv_obj_remove_flag(s_ui.speech_tail, LV_OBJ_FLAG_SCROLLABLE);

    s_ui.speech_bubble = lv_obj_create(screen);
    lv_obj_set_pos(s_ui.speech_bubble, BUBBLE_RIGHT - BUBBLE_MIN_WIDTH, BUBBLE_TOP);
    lv_obj_set_size(s_ui.speech_bubble, BUBBLE_MIN_WIDTH, 64);
    lv_obj_set_style_bg_color(s_ui.speech_bubble, lv_color_hex(0xF1F5F7), 0);
    lv_obj_set_style_bg_opa(s_ui.speech_bubble, LV_OPA_COVER, 0);
    lv_obj_set_style_border_color(s_ui.speech_bubble, lv_color_hex(0xB7C8D0), 0);
    lv_obj_set_style_border_width(s_ui.speech_bubble, 1, 0);
    lv_obj_set_style_radius(s_ui.speech_bubble, 15, 0);
    lv_obj_set_style_pad_all(s_ui.speech_bubble, 0, 0);
    lv_obj_set_style_shadow_color(s_ui.speech_bubble, lv_color_hex(0x000000), 0);
    lv_obj_set_style_shadow_opa(s_ui.speech_bubble, LV_OPA_20, 0);
    lv_obj_set_style_shadow_width(s_ui.speech_bubble, 8, 0);
    lv_obj_set_style_shadow_offset_y(s_ui.speech_bubble, 3, 0);
    lv_obj_remove_flag(s_ui.speech_bubble, LV_OBJ_FLAG_SCROLLABLE);

    s_ui.state_label = make_label(
        screen, &lv_font_montserrat_12, lv_color_hex(0x697B87), LV_TEXT_ALIGN_LEFT);
    lv_obj_set_pos(s_ui.state_label, 160, 33);
    lv_obj_set_width(s_ui.state_label, BUBBLE_MIN_WIDTH - (BUBBLE_PADDING_X * 2));
    s_ui.title_label = make_label(
        screen, &lv_font_montserrat_14, lv_color_hex(0x14242C), LV_TEXT_ALIGN_LEFT);
    lv_label_set_long_mode(s_ui.title_label, LV_LABEL_LONG_MODE_DOTS);
    lv_obj_set_pos(s_ui.title_label, 160, 49);
    lv_obj_set_size(
        s_ui.title_label,
        BUBBLE_MIN_WIDTH - (BUBBLE_PADDING_X * 2),
        lv_font_get_line_height(&lv_font_montserrat_14));
    s_ui.reason_label = make_label(
        screen, &lv_font_montserrat_12, lv_color_hex(0x526771), LV_TEXT_ALIGN_LEFT);
    lv_label_set_long_mode(s_ui.reason_label, LV_LABEL_LONG_MODE_DOTS);
    lv_obj_set_pos(s_ui.reason_label, 160, 69);
    lv_obj_set_size(
        s_ui.reason_label,
        BUBBLE_MIN_WIDTH - (BUBBLE_PADDING_X * 2),
        lv_font_get_line_height(&lv_font_montserrat_12));

    s_ui.counts_label = make_label(
        screen, &lv_font_montserrat_12, lv_color_hex(0xB9C8CF), LV_TEXT_ALIGN_RIGHT);
    lv_obj_set_pos(s_ui.counts_label, 190, 199);
    lv_obj_set_size(s_ui.counts_label, 120, 20);
    lv_obj_add_flag(s_ui.counts_label, LV_OBJ_FLAG_CLICKABLE);
    lv_obj_add_event_cb(s_ui.counts_label, counts_click_handler, LV_EVENT_RELEASED, NULL);
    s_ui.subagents_label = make_label(
        screen, &lv_font_montserrat_12, lv_color_hex(0x657A85), LV_TEXT_ALIGN_RIGHT);
    lv_obj_set_pos(s_ui.subagents_label, 184, 218);
    lv_obj_set_width(s_ui.subagents_label, 126);
    lv_obj_add_flag(s_ui.subagents_label, LV_OBJ_FLAG_CLICKABLE);
    lv_obj_add_event_cb(s_ui.subagents_label, counts_click_handler, LV_EVENT_RELEASED, NULL);

    s_ui.task_tray = lv_obj_create(screen);
    lv_obj_set_pos(s_ui.task_tray, 0, STATUS_BAR_HEIGHT);
    lv_obj_set_size(s_ui.task_tray, 320, 240 - STATUS_BAR_HEIGHT);
    lv_obj_set_style_bg_color(s_ui.task_tray, lv_color_hex(0x101E25), 0);
    lv_obj_set_style_bg_opa(s_ui.task_tray, LV_OPA_COVER, 0);
    lv_obj_set_style_border_width(s_ui.task_tray, 0, 0);
    lv_obj_set_style_radius(s_ui.task_tray, 0, 0);
    lv_obj_set_style_pad_all(s_ui.task_tray, 10, 0);
    lv_obj_remove_flag(s_ui.task_tray, LV_OBJ_FLAG_SCROLLABLE);
    lv_obj_add_flag(s_ui.task_tray, LV_OBJ_FLAG_CLICKABLE | LV_OBJ_FLAG_HIDDEN);
    lv_obj_add_event_cb(s_ui.task_tray, tray_click_handler, LV_EVENT_RELEASED, NULL);
    lv_obj_t *tray_title = make_label(
        s_ui.task_tray, &lv_font_montserrat_18, lv_color_hex(0xF1F5F7), LV_TEXT_ALIGN_LEFT);
    lv_label_set_text(tray_title, "Tasks");
    lv_obj_set_pos(tray_title, 4, 0);
    for (int i = 0; i < TRAY_TASKS_VISIBLE; ++i) {
        s_ui.tray_labels[i] = make_label(
            s_ui.task_tray,
            &lv_font_montserrat_12,
            lv_color_hex(0xB9C8CF),
            LV_TEXT_ALIGN_LEFT);
        lv_label_set_long_mode(s_ui.tray_labels[i], LV_LABEL_LONG_MODE_DOTS);
        lv_obj_set_pos(s_ui.tray_labels[i], 4, 34 + i * 24);
        lv_obj_set_size(s_ui.tray_labels[i], 290, 18);
    }

    s_ui.rendered_wifi_bars = -1;
    s_ui.rendered_battery_percent = -1;
    s_ui.status_dirty = true;
    render_main_content();
    update_status_bar();
    animate_pet();
    lv_timer_create(ui_timer_handler, UI_TIMER_MS, NULL);
}

esp_err_t app_ui_start(const app_settings_t *settings)
{
    if (settings == NULL) {
        return ESP_ERR_INVALID_ARG;
    }
    memset(&s_ui, 0, sizeof(s_ui));
    s_ui.settings = *settings;
    s_ui.wifi_rssi = -127;
    if (!pet_asset_open(&s_ui.pet_asset) ||
        s_ui.pet_asset.width != PET_WIDTH ||
        s_ui.pet_asset.height != PET_HEIGHT) {
        ESP_LOGE(TAG, "pet asset is missing or has the wrong dimensions");
        return ESP_ERR_INVALID_SIZE;
    }
    s_ui.pet_pixels = heap_caps_calloc(
        PET_WIDTH * PET_HEIGHT,
        sizeof(uint16_t),
        MALLOC_CAP_SPIRAM | MALLOC_CAP_8BIT);
    if (s_ui.pet_pixels == NULL) {
        s_ui.pet_pixels = calloc(PET_WIDTH * PET_HEIGHT, sizeof(uint16_t));
    }
    if (s_ui.pet_pixels == NULL) {
        return ESP_ERR_NO_MEM;
    }
    if (bsp_display_start() == NULL) {
        return ESP_FAIL;
    }
    ESP_RETURN_ON_ERROR(
        bsp_display_backlight_on(), TAG, "turn on display backlight");
    if (!bsp_display_lock(0)) {
        return ESP_ERR_TIMEOUT;
    }
    lv_async_call(build_ui, NULL);
    bsp_display_unlock();
    return ESP_OK;
}
