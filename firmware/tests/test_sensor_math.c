#include <assert.h>
#include <math.h>
#include <stdint.h>
#include <stdio.h>

#include "../main/sensor_math.h"

static void expect_close(float actual, float expected, float tolerance)
{
    assert(fabsf(actual - expected) <= tolerance);
}

int main(void)
{
    expect_close(sensor_math_divider_voltage(1.0f), 4.01f, 0.001f);
    expect_close(sensor_math_divider_voltage(0.975f), 3.90975f, 0.001f);
    expect_close(sensor_math_c_to_f(0.0f), 32.0f, 0.001f);
    expect_close(sensor_math_c_to_f(22.4f), 72.32f, 0.001f);

    assert(sensor_math_battery_percent(3.2f) == 0);
    assert(sensor_math_battery_percent(3.9f) == 65);
    assert(sensor_math_battery_percent(4.2f) == 100);
    assert(sensor_math_battery_percent(3.95f) == 73);
    assert(!sensor_math_battery_present(0.0f));
    assert(!sensor_math_battery_present(2.99f));
    assert(sensor_math_battery_present(3.91f));

    assert(sensor_math_environment_valid(22.4f, 43.1f));
    assert(!sensor_math_environment_valid(NAN, 43.1f));
    assert(!sensor_math_environment_valid(90.0f, 43.1f));
    assert(!sensor_math_environment_valid(22.4f, 101.0f));

    assert(!sensor_math_is_stale(299000000, 1, 300000000));
    assert(sensor_math_is_stale(300000002, 1, 300000000));
    assert(sensor_math_is_stale(100, 0, 300000000));

    assert(sensor_math_presence_active(30000001, 1, 30000000));
    assert(!sensor_math_presence_active(30000002, 1, 30000000));
    assert(sensor_math_radar_edge_allowed(1000000, 0, 200000));
    assert(!sensor_math_radar_edge_allowed(1100000, 1000000, 200000));
    assert(sensor_math_radar_edge_allowed(1200000, 1000000, 200000));
    assert(sensor_math_epoch_if_valid(0) == 0);
    assert(sensor_math_epoch_if_valid(1699999999) == 0);
    assert(sensor_math_epoch_if_valid(1785100000) == 1785100000);

    puts("sensor_math tests passed");
    return 0;
}
