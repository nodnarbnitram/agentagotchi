#include "app_sensors.h"

#include <math.h>
#include <string.h>
#include <time.h>

#include "aht30.h"
#include "app_network.h"
#include "app_state.h"
#include "bsp/esp-bsp.h"
#include "driver/gpio.h"
#include "driver/i2c_master.h"
#include "esp_adc/adc_cali.h"
#include "esp_adc/adc_cali_scheme.h"
#include "esp_adc/adc_oneshot.h"
#include "esp_check.h"
#include "esp_heap_caps.h"
#include "esp_log.h"
#include "esp_timer.h"
#include "freertos/FreeRTOS.h"
#include "freertos/idf_additions.h"
#include "freertos/task.h"
#include "sensor_math.h"

/* The BSP owns both controllers: its secondary controller is routed to the
 * SENSOR dock on GPIO40/GPIO41. */
#define SENSOR_I2C_PORT (BSP_I2C_NUM == I2C_NUM_1 ? I2C_NUM_0 : I2C_NUM_1)
#define RADAR_GPIO GPIO_NUM_21
#define BATTERY_CHANNEL ADC_CHANNEL_9
#define ENV_INTERVAL_US (30LL * 1000000LL)
#define BATTERY_INTERVAL_US (60LL * 1000000LL)
#define SENSOR_STALE_US (5LL * 60LL * 1000000LL)
#define PRESENCE_HOLD_US (30LL * 1000000LL)
#define RADAR_DEBOUNCE_US (200LL * 1000LL)
#define BATTERY_SAMPLES 64

static const char *TAG = "pet_sensors";
static TaskHandle_t s_sensor_task;

typedef struct {
    i2c_master_bus_handle_t i2c_bus;
    aht30_handle_t aht;
    adc_oneshot_unit_handle_t adc;
    adc_cali_handle_t adc_cali;
    bool adc_calibrated;
    app_sensor_state_t state;
    int64_t last_env_attempt_us;
    int64_t last_env_good_us;
    int64_t last_battery_attempt_us;
    int64_t last_presence_us;
    int64_t last_radar_edge_us;
    int environment_failures;
    bool battery_filter_valid;
    float filtered_battery_voltage;
} sensor_context_t;

static void IRAM_ATTR radar_isr(void *argument)
{
    BaseType_t high_priority_task_woken = pdFALSE;
    vTaskNotifyGiveFromISR((TaskHandle_t)argument, &high_priority_task_woken);
    if (high_priority_task_woken) {
        portYIELD_FROM_ISR();
    }
}

static void post_state(const sensor_context_t *context)
{
    app_ui_event_t event = {
        .type = APP_UI_EVENT_SENSOR,
        .data.sensor = context->state,
    };
    app_ui_post(&event);
}

static int64_t wall_clock_seconds(void)
{
    if (!app_network_wall_clock_valid()) {
        return 0;
    }
    return sensor_math_epoch_if_valid((int64_t)time(NULL));
}

static void init_radar(void)
{
    gpio_config_t config = {
        .pin_bit_mask = 1ULL << RADAR_GPIO,
        .mode = GPIO_MODE_INPUT,
        .pull_up_en = GPIO_PULLUP_DISABLE,
        .pull_down_en = GPIO_PULLDOWN_ENABLE,
        .intr_type = GPIO_INTR_POSEDGE,
    };
    ESP_ERROR_CHECK_WITHOUT_ABORT(gpio_config(&config));
    esp_err_t err = gpio_install_isr_service(0);
    if (err != ESP_OK && err != ESP_ERR_INVALID_STATE) {
        ESP_LOGW(TAG, "radar ISR service unavailable: %s", esp_err_to_name(err));
        return;
    }
    ESP_ERROR_CHECK_WITHOUT_ABORT(gpio_isr_handler_add(RADAR_GPIO, radar_isr, s_sensor_task));
}

static void init_environment(sensor_context_t *context)
{
    esp_err_t err = bsp_i2c_init();
    if (err == ESP_OK) {
        err = i2c_master_get_bus_handle(SENSOR_I2C_PORT, &context->i2c_bus);
    }
    if (err != ESP_OK) {
        ESP_LOGW(TAG, "AHT30 I2C bus unavailable: %s", esp_err_to_name(err));
        context->i2c_bus = NULL;
        return;
    }
    err = aht30_create(context->i2c_bus, AHT30_I2C_ADDRESS, &context->aht);
    if (err != ESP_OK) {
        context->aht = NULL;
        ESP_LOGW(TAG, "AHT30 unavailable: %s", esp_err_to_name(err));
    }
}

static void recover_environment(sensor_context_t *context)
{
    if (context->aht != NULL) {
        aht30_delete(context->aht);
        context->aht = NULL;
    }
    if (context->i2c_bus != NULL) {
        (void)i2c_master_bus_reset(context->i2c_bus);
    }
    vTaskDelay(pdMS_TO_TICKS(20));
    if (context->i2c_bus != NULL) {
        esp_err_t err = aht30_create(
            context->i2c_bus, AHT30_I2C_ADDRESS, &context->aht);
        if (err != ESP_OK) {
            context->aht = NULL;
            ESP_LOGW(TAG, "AHT30 recovery failed: %s", esp_err_to_name(err));
        }
    } else {
        init_environment(context);
    }
    context->environment_failures = 0;
}

static void init_battery(sensor_context_t *context)
{
    const adc_oneshot_unit_init_cfg_t unit_config = {
        .unit_id = ADC_UNIT_1,
        .ulp_mode = ADC_ULP_MODE_DISABLE,
    };
    if (adc_oneshot_new_unit(&unit_config, &context->adc) != ESP_OK) {
        context->adc = NULL;
        return;
    }
    const adc_oneshot_chan_cfg_t channel_config = {
        .bitwidth = ADC_BITWIDTH_DEFAULT,
        .atten = ADC_ATTEN_DB_12,
    };
    if (adc_oneshot_config_channel(context->adc, BATTERY_CHANNEL, &channel_config) != ESP_OK) {
        adc_oneshot_del_unit(context->adc);
        context->adc = NULL;
        return;
    }
    const adc_cali_curve_fitting_config_t calibration_config = {
        .unit_id = ADC_UNIT_1,
        .chan = BATTERY_CHANNEL,
        .atten = ADC_ATTEN_DB_12,
        .bitwidth = ADC_BITWIDTH_DEFAULT,
    };
    context->adc_calibrated =
        adc_cali_create_scheme_curve_fitting(&calibration_config, &context->adc_cali) == ESP_OK;
    if (!context->adc_calibrated) {
        ESP_LOGW(TAG, "ADC calibration unavailable; battery estimate disabled");
    }
}

static bool read_environment(sensor_context_t *context, int64_t now)
{
    context->last_env_attempt_us = now;
    if (context->aht == NULL) {
        context->environment_failures++;
        if (sensor_math_is_stale(now, context->last_env_good_us, SENSOR_STALE_US)) {
            context->state.temperature_valid = false;
            context->state.humidity_valid = false;
        }
        if (context->environment_failures >= 3) {
            recover_environment(context);
        }
        return false;
    }
    float temperature = NAN;
    float humidity = NAN;
    esp_err_t err = aht30_get_temperature_humidity_value(
        context->aht, &temperature, &humidity);
    if (err != ESP_OK || !sensor_math_environment_valid(temperature, humidity)) {
        context->environment_failures++;
        if (sensor_math_is_stale(now, context->last_env_good_us, SENSOR_STALE_US)) {
            context->state.temperature_valid = false;
            context->state.humidity_valid = false;
        }
        if (context->environment_failures >= 3) {
            ESP_LOGW(TAG, "recovering AHT30 I2C bus after repeated read failures");
            recover_environment(context);
        }
        return false;
    }
    context->state.temperature_c = temperature;
    context->state.humidity_rh = humidity;
    context->state.temperature_valid = true;
    context->state.humidity_valid = true;
    context->last_env_good_us = now;
    context->environment_failures = 0;
    return true;
}

static bool read_battery(sensor_context_t *context, int64_t now)
{
    context->last_battery_attempt_us = now;
    if (context->adc == NULL) {
        context->state.battery_valid = false;
        return false;
    }
    int64_t raw_sum = 0;
    int successful = 0;
    for (int i = 0; i < BATTERY_SAMPLES; ++i) {
        int raw = 0;
        if (adc_oneshot_read(context->adc, BATTERY_CHANNEL, &raw) == ESP_OK) {
            raw_sum += raw;
            successful++;
        }
        vTaskDelay(pdMS_TO_TICKS(2));
    }
    if (successful < BATTERY_SAMPLES / 2) {
        context->state.battery_valid = false;
        return false;
    }
    int raw_average = (int)(raw_sum / successful);
    if (!context->adc_calibrated) {
        context->state.battery_valid = false;
        return false;
    }
    int millivolts = 0;
    if (adc_cali_raw_to_voltage(context->adc_cali, raw_average, &millivolts) != ESP_OK) {
        context->state.battery_valid = false;
        return false;
    }
    float voltage = sensor_math_divider_voltage((float)millivolts / 1000.0f);
    context->state.battery_valid = sensor_math_battery_present(voltage);
    if (context->state.battery_valid) {
        if (context->battery_filter_valid) {
            context->filtered_battery_voltage =
                context->filtered_battery_voltage * 0.75f + voltage * 0.25f;
        } else {
            context->filtered_battery_voltage = voltage;
            context->battery_filter_valid = true;
        }
        context->state.battery_voltage = context->filtered_battery_voltage;
        context->state.battery_percent =
            sensor_math_battery_percent(context->filtered_battery_voltage);
    } else {
        context->battery_filter_valid = false;
    }
    return context->state.battery_valid;
}

static void sensor_task(void *argument)
{
    sensor_context_t context = {0};
    context.state.battery_estimate = true;
    s_sensor_task = xTaskGetCurrentTaskHandle();
    init_radar();
    init_environment(&context);
    init_battery(&context);
    int64_t now = esp_timer_get_time();
    (void)read_environment(&context, now);
    (void)read_battery(&context, now);
    context.state.sensor_updated_us = now;
    context.state.sensor_updated_at = wall_clock_seconds();
    post_state(&context);

    while (true) {
        uint32_t notifications = ulTaskNotifyTake(pdTRUE, pdMS_TO_TICKS(1000));
        now = esp_timer_get_time();
        bool changed = false;
        if (notifications > 0 &&
            sensor_math_radar_edge_allowed(
                now, context.last_radar_edge_us, RADAR_DEBOUNCE_US)) {
            context.last_radar_edge_us = now;
            context.last_presence_us = now;
        }
        if (gpio_get_level(RADAR_GPIO) != 0) {
            context.last_presence_us = now;
        }
        bool presence = sensor_math_presence_active(
            now, context.last_presence_us, PRESENCE_HOLD_US);
        if (presence != context.state.presence) {
            context.state.presence = presence;
            changed = true;
        }
        if (context.last_env_attempt_us == 0 || now - context.last_env_attempt_us >= ENV_INTERVAL_US) {
            (void)read_environment(&context, now);
            changed = true;
        }
        if (context.last_battery_attempt_us == 0 ||
            now - context.last_battery_attempt_us >= BATTERY_INTERVAL_US) {
            (void)read_battery(&context, now);
            changed = true;
        }
        if (changed) {
            context.state.sensor_updated_us = now;
            context.state.sensor_updated_at = wall_clock_seconds();
#if CONFIG_CODEX_PET_LOG_SENSOR_DIAGNOSTICS
            ESP_LOGI(TAG, "T=%.2fC RH=%.1f%% battery=%.3fV/%d%% presence=%d",
                context.state.temperature_c, context.state.humidity_rh,
                context.state.battery_voltage, context.state.battery_percent,
                context.state.presence);
#endif
            post_state(&context);
        }
    }
}

esp_err_t app_sensors_start(void)
{
#if CONFIG_CODEX_PET_SENSOR_BAR
    if (xTaskCreatePinnedToCoreWithCaps(
            sensor_task,
            "pet_sensors",
            6144,
            NULL,
            3,
            &s_sensor_task,
            0,
            MALLOC_CAP_SPIRAM | MALLOC_CAP_8BIT) != pdPASS) {
        return ESP_ERR_NO_MEM;
    }
#else
    /* Keep the disabled configuration warning-clean while allowing the linker
     * to discard the complete sensor implementation. */
    (void)sensor_task;
#endif
    return ESP_OK;
}
