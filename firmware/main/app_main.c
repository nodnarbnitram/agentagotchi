#include <stdlib.h>
#include <string.h>
#include <sys/time.h>
#include <time.h>

#include "app_audio.h"
#include "app_network.h"
#include "app_sensors.h"
#include "app_settings.h"
#include "app_state.h"
#include "app_ui.h"
#include "esp_check.h"
#include "esp_heap_caps.h"
#include "esp_log.h"
#include "nvs_flash.h"

#define UI_QUEUE_DEPTH 8

static const char *TAG = "codex_pet";
static QueueHandle_t s_ui_queue;
static StaticQueue_t s_ui_queue_control;
static uint8_t *s_ui_queue_storage;

QueueHandle_t app_ui_queue(void)
{
    return s_ui_queue;
}

void app_ui_post(const app_ui_event_t *event)
{
    if (s_ui_queue == NULL || event == NULL) {
        return;
    }
    if (xQueueSend(s_ui_queue, event, 0) != pdTRUE) {
        /* app_ui_event_t contains a full task snapshot and is too large for
         * the smaller producer-task stacks. Allocate the rare overflow
         * scratch buffer instead of placing it on a caller's stack. */
        app_ui_event_t *discarded = malloc(sizeof(*discarded));
        if (discarded != NULL) {
            (void)xQueueReceive(s_ui_queue, discarded, 0);
            free(discarded);
            (void)xQueueSend(s_ui_queue, event, 0);
        }
    }
}

static esp_err_t initialize_nvs(void)
{
    esp_err_t err = nvs_flash_init();
    if (err == ESP_ERR_NVS_NO_FREE_PAGES ||
        err == ESP_ERR_NVS_NEW_VERSION_FOUND) {
        ESP_RETURN_ON_ERROR(nvs_flash_erase(), TAG, "erase incompatible NVS");
        err = nvs_flash_init();
    }
    return err;
}

static void apply_tls_bootstrap_time(const app_settings_t *settings)
{
    if (settings->tls_bootstrap_epoch < 1700000000 ||
        settings->tls_bootstrap_epoch > 4102444800) {
        return;
    }
    struct timeval now;
    if (gettimeofday(&now, NULL) == 0 &&
        now.tv_sec >= settings->tls_bootstrap_epoch) {
        return;
    }
    const struct timeval bootstrap = {
        .tv_sec = (time_t)settings->tls_bootstrap_epoch,
        .tv_usec = 0,
    };
    if (settimeofday(&bootstrap, NULL) != 0) {
        ESP_LOGW(TAG, "could not apply TLS bootstrap time");
    }
}

void app_main(void)
{
    ESP_ERROR_CHECK(initialize_nvs());
    s_ui_queue_storage = heap_caps_malloc(
        UI_QUEUE_DEPTH * sizeof(app_ui_event_t),
        MALLOC_CAP_SPIRAM | MALLOC_CAP_8BIT);
    ESP_ERROR_CHECK(s_ui_queue_storage == NULL ? ESP_ERR_NO_MEM : ESP_OK);
    s_ui_queue = xQueueCreateStatic(
        UI_QUEUE_DEPTH,
        sizeof(app_ui_event_t),
        s_ui_queue_storage,
        &s_ui_queue_control);
    ESP_ERROR_CHECK(s_ui_queue == NULL ? ESP_ERR_NO_MEM : ESP_OK);

    /* The pinned certificate makes this structure larger than the default
     * ESP-IDF main-task stack, so keep it in static storage. */
    static app_settings_t settings;
    ESP_ERROR_CHECK(app_settings_load(&settings));
    apply_tls_bootstrap_time(&settings);
    ESP_ERROR_CHECK(app_ui_start(&settings));
    ESP_ERROR_CHECK(app_audio_start());
    ESP_ERROR_CHECK(app_sensors_start());

    if (!settings.configured) {
        /* Static because an event includes the complete task snapshot union. */
        static app_ui_event_t event;
        memset(&event, 0, sizeof(event));
        event.type = APP_UI_EVENT_PROVISIONING;
        event.data.provisioning.waiting = true;
        event.data.provisioning.temp_unit = settings.temp_unit;
        app_ui_post(&event);
        ESP_ERROR_CHECK(app_settings_wait_for_provision(&settings));
        apply_tls_bootstrap_time(&settings);
        event.data.provisioning.waiting = false;
        event.data.provisioning.temp_unit = settings.temp_unit;
        app_ui_post(&event);
    }
    ESP_ERROR_CHECK(app_network_start(&settings));
}
