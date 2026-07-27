#include "app_settings.h"

#include <stdio.h>
#include <stdlib.h>
#include <string.h>

#include "cJSON.h"
#include "driver/usb_serial_jtag.h"
#include "driver/usb_serial_jtag_vfs.h"
#include "esp_log.h"
#include "freertos/FreeRTOS.h"
#include "freertos/task.h"
#include "nvs.h"
#include "nvs_flash.h"

#define PROVISION_PREFIX "CODEX_PET_PROVISION "
#define PROVISION_BUFFER_SIZE 8192

static const char *TAG = "pet_settings";
static const char *NAMESPACE = "codex_pet";

static void secure_clear(void *memory, size_t length)
{
    volatile unsigned char *bytes = memory;
    while (length-- > 0) {
        *bytes++ = 0;
    }
}

static esp_err_t read_string(nvs_handle_t nvs, const char *key, char *out, size_t capacity)
{
    size_t required = capacity;
    esp_err_t err = nvs_get_str(nvs, key, out, &required);
    if (err == ESP_ERR_NVS_NOT_FOUND) {
        out[0] = '\0';
        return ESP_OK;
    }
    return err;
}

esp_err_t app_settings_load(app_settings_t *settings)
{
    if (settings == NULL) {
        return ESP_ERR_INVALID_ARG;
    }
    memset(settings, 0, sizeof(*settings));
    settings->bridge_port = 8787;
    settings->temp_unit = 'F';

    nvs_handle_t nvs;
    esp_err_t err = nvs_open(NAMESPACE, NVS_READONLY, &nvs);
    if (err == ESP_ERR_NVS_NOT_FOUND) {
        return ESP_OK;
    }
    if (err != ESP_OK) {
        return err;
    }
    uint8_t configured = 0;
    (void)nvs_get_u8(nvs, "configured", &configured);
    (void)read_string(nvs, "ssid", settings->ssid, sizeof(settings->ssid));
    (void)read_string(nvs, "password", settings->password, sizeof(settings->password));
    (void)read_string(nvs, "host", settings->bridge_host, sizeof(settings->bridge_host));
    (void)read_string(nvs, "token", settings->token, sizeof(settings->token));
    (void)read_string(nvs, "ca_pem", settings->ca_pem, sizeof(settings->ca_pem));
    int32_t port = settings->bridge_port;
    (void)nvs_get_i32(nvs, "port", &port);
    settings->bridge_port = (int)port;
    uint8_t unit = (uint8_t)settings->temp_unit;
    (void)nvs_get_u8(nvs, "temp_unit", &unit);
    settings->temp_unit = (char)unit;
    (void)nvs_get_i64(nvs, "tls_epoch", &settings->tls_bootstrap_epoch);
    nvs_close(nvs);

    settings->configured = configured == 1 &&
        settings->ssid[0] != '\0' &&
        settings->bridge_host[0] != '\0' &&
        settings->token[0] != '\0' &&
        settings->ca_pem[0] != '\0' &&
        settings->bridge_port > 0 && settings->bridge_port <= 65535;
    return ESP_OK;
}

static esp_err_t save_settings(const app_settings_t *settings)
{
    nvs_handle_t nvs;
    esp_err_t err = nvs_open(NAMESPACE, NVS_READWRITE, &nvs);
    if (err != ESP_OK) {
        return err;
    }
    if ((err = nvs_set_str(nvs, "ssid", settings->ssid)) != ESP_OK ||
        (err = nvs_set_str(nvs, "password", settings->password)) != ESP_OK ||
        (err = nvs_set_str(nvs, "host", settings->bridge_host)) != ESP_OK ||
        (err = nvs_set_i32(nvs, "port", settings->bridge_port)) != ESP_OK ||
        (err = nvs_set_str(nvs, "token", settings->token)) != ESP_OK ||
        (err = nvs_set_str(nvs, "ca_pem", settings->ca_pem)) != ESP_OK ||
        (err = nvs_set_u8(nvs, "temp_unit", (uint8_t)settings->temp_unit)) != ESP_OK ||
        (err = nvs_set_i64(nvs, "tls_epoch", settings->tls_bootstrap_epoch)) != ESP_OK ||
        (err = nvs_set_u8(nvs, "configured", 1)) != ESP_OK ||
        (err = nvs_commit(nvs)) != ESP_OK) {
        nvs_close(nvs);
        return err;
    }
    nvs_close(nvs);
    return ESP_OK;
}

static bool copy_json_string(cJSON *root, const char *key, char *out, size_t capacity, bool required)
{
    cJSON *value = cJSON_GetObjectItemCaseSensitive(root, key);
    if (!cJSON_IsString(value) || value->valuestring == NULL) {
        return !required;
    }
    size_t length = strlen(value->valuestring);
    if (length >= capacity) {
        return false;
    }
    memcpy(out, value->valuestring, length + 1);
    return true;
}

static esp_err_t parse_provision(const char *line, app_settings_t *settings)
{
    size_t prefix_length = strlen(PROVISION_PREFIX);
    if (strncmp(line, PROVISION_PREFIX, prefix_length) != 0) {
        return ESP_ERR_INVALID_ARG;
    }
    cJSON *root = cJSON_Parse(line + prefix_length);
    if (root == NULL) {
        return ESP_ERR_INVALID_ARG;
    }
    cJSON *type = cJSON_GetObjectItemCaseSensitive(root, "type");
    cJSON *version = cJSON_GetObjectItemCaseSensitive(root, "version");
    cJSON *port = cJSON_GetObjectItemCaseSensitive(root, "bridgePort");
    cJSON *unit = cJSON_GetObjectItemCaseSensitive(root, "tempUnit");
    cJSON *unix_time = cJSON_GetObjectItemCaseSensitive(root, "unixTime");
    bool valid = cJSON_IsString(type) && strcmp(type->valuestring, "provision") == 0 &&
        cJSON_IsNumber(version) && version->valueint == 1 &&
        cJSON_IsNumber(port) && port->valueint > 0 && port->valueint <= 65535 &&
        copy_json_string(root, "ssid", settings->ssid, sizeof(settings->ssid), true) &&
        copy_json_string(root, "password", settings->password, sizeof(settings->password), false) &&
        copy_json_string(root, "bridgeHost", settings->bridge_host, sizeof(settings->bridge_host), true) &&
        copy_json_string(root, "token", settings->token, sizeof(settings->token), true) &&
        copy_json_string(root, "caPem", settings->ca_pem, sizeof(settings->ca_pem), true);
    if (valid) {
        settings->bridge_port = port->valueint;
        settings->temp_unit = 'F';
        if (cJSON_IsString(unit) && unit->valuestring != NULL && unit->valuestring[0] == 'C') {
            settings->temp_unit = 'C';
        }
        if (cJSON_IsNumber(unix_time) &&
            unix_time->valuedouble >= 1700000000.0 &&
            unix_time->valuedouble <= 4102444800.0) {
            settings->tls_bootstrap_epoch = (int64_t)unix_time->valuedouble;
        }
        settings->configured = true;
    }
    cJSON_Delete(root);
    return valid ? ESP_OK : ESP_ERR_INVALID_ARG;
}

esp_err_t app_settings_wait_for_provision(app_settings_t *settings)
{
    if (settings == NULL) {
        return ESP_ERR_INVALID_ARG;
    }

    /*
     * The ROM USB Serial/JTAG VFS is sufficient for log output but its
     * non-blocking input path can drop a provisioning record while the host
     * reconnects after reset. Switch stdin/stdout to the interrupt-driven
     * driver and give RX enough room for the pinned CA certificate.
     */
    usb_serial_jtag_driver_config_t usb_config = {
        .tx_buffer_size = 1024,
        .rx_buffer_size = PROVISION_BUFFER_SIZE,
    };
    esp_err_t usb_err = usb_serial_jtag_driver_install(&usb_config);
    if (usb_err != ESP_OK) {
        ESP_LOGE(TAG, "could not install USB Serial/JTAG driver: %s", esp_err_to_name(usb_err));
        return usb_err;
    }
    usb_serial_jtag_vfs_set_rx_line_endings(ESP_LINE_ENDINGS_CRLF);
    usb_serial_jtag_vfs_set_tx_line_endings(ESP_LINE_ENDINGS_CRLF);
    usb_serial_jtag_vfs_use_driver();

    char *line = malloc(PROVISION_BUFFER_SIZE);
    app_settings_t *candidate = calloc(1, sizeof(*candidate));
    if (line == NULL || candidate == NULL) {
        free(line);
        free(candidate);
        return ESP_ERR_NO_MEM;
    }
    ESP_LOGI(TAG, "waiting for USB provisioning");
    size_t line_length = 0;
    for (;;) {
        char byte = '\0';
        if (usb_serial_jtag_read_bytes(&byte, 1, portMAX_DELAY) != 1) {
            continue;
        }
        if (byte != '\r' && byte != '\n') {
            if (line_length < PROVISION_BUFFER_SIZE - 1) {
                line[line_length++] = byte;
            } else {
                secure_clear(line, PROVISION_BUFFER_SIZE);
                line_length = 0;
            }
            continue;
        }
        if (line_length == 0) {
            continue;
        }
        line[line_length] = '\0';
        line_length = 0;
        secure_clear(candidate, sizeof(*candidate));
        if (parse_provision(line, candidate) != ESP_OK) {
            secure_clear(line, PROVISION_BUFFER_SIZE);
            continue;
        }
        esp_err_t err = save_settings(candidate);
        if (err == ESP_OK) {
            *settings = *candidate;
            ESP_LOGI(TAG, "provisioning accepted");
            secure_clear(line, PROVISION_BUFFER_SIZE);
            secure_clear(candidate, sizeof(*candidate));
            free(line);
            free(candidate);
            return ESP_OK;
        }
        ESP_LOGE(TAG, "could not save provisioning data: %s", esp_err_to_name(err));
    }
}
