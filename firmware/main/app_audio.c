#include "app_audio.h"

#include <math.h>
#include <stdint.h>
#include <stdlib.h>

#include "bsp/esp-bsp.h"
#include "driver/i2c_master.h"
#include "es8311_codec.h"
#include "esp_codec_dev.h"
#include "esp_heap_caps.h"
#include "esp_log.h"
#include "freertos/FreeRTOS.h"
#include "freertos/idf_additions.h"
#include "freertos/queue.h"
#include "freertos/task.h"

#define CHIRP_SAMPLE_RATE 16000
#define CHIRP_DURATION_MS 140
#define CHIRP_FREQUENCY_HZ 880.0f
#define CHIRP_VOLUME 24
#define APP_PI 3.14159265358979323846f
#define CODEC_STARTUP_DELAY_MS 1000
#define CODEC_PROBE_INTERVAL_MS 250
#define CODEC_PROBE_TIMEOUT_MS 100
#define CODEC_PROBE_ATTEMPTS 40
#define CODEC_STABLE_PROBES 3

static const char *TAG = "pet_audio";
static QueueHandle_t s_chirp_queue;

static bool codec_is_ready(void)
{
    /* The ES8311 can take longer than the display/touch controller to become
     * responsive after a cold boot. The BSP initializer asserts if its first
     * codec transaction fails, so require several consecutive acknowledgments
     * before entering it. A missing or slow codec disables only the chirp. */
    vTaskDelay(pdMS_TO_TICKS(CODEC_STARTUP_DELAY_MS));
    i2c_master_bus_handle_t bus = bsp_i2c_get_handle();
    if (bus == NULL) {
        return false;
    }
    int consecutive = 0;
    for (int attempt = 0; attempt < CODEC_PROBE_ATTEMPTS; ++attempt) {
        esp_err_t err = i2c_master_probe(
            bus,
            ES8311_CODEC_DEFAULT_ADDR >> 1,
            CODEC_PROBE_TIMEOUT_MS);
        if (err == ESP_OK) {
            consecutive++;
            if (consecutive >= CODEC_STABLE_PROBES) {
                return true;
            }
        } else {
            consecutive = 0;
        }
        vTaskDelay(pdMS_TO_TICKS(CODEC_PROBE_INTERVAL_MS));
    }
    return false;
}

static void audio_task(void *argument)
{
    (void)argument;
    esp_codec_dev_handle_t speaker = NULL;
    if (codec_is_ready()) {
        speaker = bsp_audio_codec_speaker_init();
    }
    if (speaker == NULL) {
        ESP_LOGW(TAG, "speaker codec unavailable; chirps disabled");
    }
    esp_codec_dev_sample_info_t format = {
        .sample_rate = CHIRP_SAMPLE_RATE,
        .channel = 1,
        .bits_per_sample = 16,
    };
    if (speaker != NULL) {
        (void)esp_codec_dev_set_out_vol(speaker, CHIRP_VOLUME);
        if (esp_codec_dev_open(speaker, &format) != 0) {
            ESP_LOGW(TAG, "speaker stream unavailable");
            speaker = NULL;
        }
    }

    const size_t sample_count = (CHIRP_SAMPLE_RATE * CHIRP_DURATION_MS) / 1000;
    int16_t *samples = calloc(sample_count, sizeof(int16_t));
    if (samples != NULL) {
        for (size_t i = 0; i < sample_count; ++i) {
            float progress = (float)i / (float)sample_count;
            float envelope = sinf(progress * APP_PI);
            float phase = 2.0f * APP_PI * CHIRP_FREQUENCY_HZ *
                ((float)i / CHIRP_SAMPLE_RATE);
            samples[i] = (int16_t)(sinf(phase) * envelope * 5500.0f);
        }
    }

    bool request = false;
    while (true) {
        if (xQueueReceive(s_chirp_queue, &request, portMAX_DELAY) == pdTRUE &&
            request && speaker != NULL && samples != NULL) {
            (void)esp_codec_dev_write(speaker, samples, sample_count * sizeof(int16_t));
        }
    }
}

esp_err_t app_audio_start(void)
{
    s_chirp_queue = xQueueCreate(1, sizeof(bool));
    if (s_chirp_queue == NULL) {
        return ESP_ERR_NO_MEM;
    }
    if (xTaskCreatePinnedToCoreWithCaps(
            audio_task,
            "pet_audio",
            6144,
            NULL,
            3,
            NULL,
            0,
            MALLOC_CAP_SPIRAM | MALLOC_CAP_8BIT) != pdPASS) {
        vQueueDelete(s_chirp_queue);
        s_chirp_queue = NULL;
        return ESP_ERR_NO_MEM;
    }
    return ESP_OK;
}

void app_audio_request_chirp(void)
{
    if (s_chirp_queue == NULL) {
        return;
    }
    bool request = true;
    (void)xQueueOverwrite(s_chirp_queue, &request);
}
