#pragma once

#include <stdbool.h>
#include <stddef.h>
#include <stdint.h>

#include "esp_err.h"

#define APP_SSID_MAX 33
#define APP_WIFI_PASSWORD_MAX 65
#define APP_HOST_MAX 128
#define APP_TOKEN_MAX 80
#define APP_CA_PEM_MAX 4096

typedef struct {
    bool configured;
    char ssid[APP_SSID_MAX];
    char password[APP_WIFI_PASSWORD_MAX];
    char bridge_host[APP_HOST_MAX];
    int bridge_port;
    char token[APP_TOKEN_MAX];
    char ca_pem[APP_CA_PEM_MAX];
    char temp_unit;
    /* Persisted Mac time used only to bootstrap TLS certificate validation. */
    int64_t tls_bootstrap_epoch;
} app_settings_t;

esp_err_t app_settings_load(app_settings_t *settings);
esp_err_t app_settings_wait_for_provision(app_settings_t *settings);
