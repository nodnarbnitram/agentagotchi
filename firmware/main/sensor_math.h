#pragma once

#include <stdbool.h>
#include <stdint.h>

float sensor_math_divider_voltage(float adc_voltage);
int sensor_math_battery_percent(float voltage);
float sensor_math_c_to_f(float celsius);
bool sensor_math_environment_valid(float temperature_c, float humidity_rh);
bool sensor_math_battery_present(float cell_voltage);
bool sensor_math_is_stale(int64_t now_us, int64_t last_good_us, int64_t stale_after_us);
bool sensor_math_presence_active(int64_t now_us, int64_t last_detection_us, int64_t hold_us);
bool sensor_math_radar_edge_allowed(int64_t now_us, int64_t last_edge_us, int64_t debounce_us);
int64_t sensor_math_epoch_if_valid(int64_t wall_seconds);
