#include "sensor_math.h"

#include <math.h>
#include <stddef.h>

typedef struct {
    float voltage;
    int percent;
} battery_point_t;

static const battery_point_t BATTERY_CURVE[] = {
    {3.30f, 0},
    {3.45f, 3},
    {3.60f, 10},
    {3.70f, 25},
    {3.80f, 45},
    {3.90f, 65},
    {4.00f, 80},
    {4.10f, 90},
    {4.20f, 100},
};

float sensor_math_divider_voltage(float adc_voltage)
{
    return adc_voltage * ((301.0f + 100.0f) / 100.0f);
}

int sensor_math_battery_percent(float voltage)
{
    if (!isfinite(voltage) || voltage <= BATTERY_CURVE[0].voltage) {
        return 0;
    }
    size_t count = sizeof(BATTERY_CURVE) / sizeof(BATTERY_CURVE[0]);
    if (voltage >= BATTERY_CURVE[count - 1].voltage) {
        return 100;
    }
    for (size_t i = 1; i < count; ++i) {
        if (voltage <= BATTERY_CURVE[i].voltage) {
            const battery_point_t *low = &BATTERY_CURVE[i - 1];
            const battery_point_t *high = &BATTERY_CURVE[i];
            float ratio = (voltage - low->voltage) / (high->voltage - low->voltage);
            return (int)lroundf(low->percent + ratio * (high->percent - low->percent));
        }
    }
    return 100;
}

float sensor_math_c_to_f(float celsius)
{
    return celsius * 9.0f / 5.0f + 32.0f;
}

bool sensor_math_environment_valid(float temperature_c, float humidity_rh)
{
    return isfinite(temperature_c) && isfinite(humidity_rh) &&
        temperature_c >= -40.0f && temperature_c <= 85.0f &&
        humidity_rh >= 0.0f && humidity_rh <= 100.0f;
}

bool sensor_math_battery_present(float cell_voltage)
{
    return isfinite(cell_voltage) && cell_voltage >= 3.0f && cell_voltage <= 4.35f;
}

bool sensor_math_is_stale(int64_t now_us, int64_t last_good_us, int64_t stale_after_us)
{
    return last_good_us <= 0 || stale_after_us <= 0 || now_us < last_good_us ||
        now_us - last_good_us > stale_after_us;
}

bool sensor_math_presence_active(int64_t now_us, int64_t last_detection_us, int64_t hold_us)
{
    return last_detection_us > 0 && hold_us > 0 && now_us >= last_detection_us &&
        now_us - last_detection_us <= hold_us;
}

bool sensor_math_radar_edge_allowed(int64_t now_us, int64_t last_edge_us, int64_t debounce_us)
{
    return last_edge_us <= 0 || now_us < last_edge_us || now_us - last_edge_us >= debounce_us;
}

int64_t sensor_math_epoch_if_valid(int64_t wall_seconds)
{
    /* Reject reset-time clocks and other obviously false Unix timestamps. */
    return wall_seconds >= 1700000000 ? wall_seconds : 0;
}
