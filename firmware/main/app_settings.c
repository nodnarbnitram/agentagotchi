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

#define PROVISION_PREFIX "AGOT_PROVISION "
#define PROVISION_BUFFER_SIZE 8192

static const char *TAG = "pet_settings";
static const char *NAMESPACE = "agentagotchi";

static void secure_clear(void *memory, size_t length)
{
    volatile unsigned char *bytes = memory;
    while (length-- > 0) {
        *bytes++ = 0;
    }
}

static esp_err_t read_string(nvs_handle_t nvs, const char *key, char *out, size_t capacity)
{
    if (out == NULL || capacity == 0) {
        return ESP_ERR_INVALID_ARG;
    }
    out[0] = '\0';
    size_t required = capacity;
    esp_err_t err = nvs_get_str(nvs, key, out, &required);
    if (err == ESP_ERR_NVS_NOT_FOUND) {
        return ESP_OK;
    }
    return err;
}

static bool pairing_has_credentials(const app_feed_pairing_t *pairing)
{
    return pairing != NULL && pairing->host[0] != '\0' && pairing->token[0] != '\0' &&
        pairing->ca_pem[0] != '\0' && pairing->port > 0 && pairing->port <= 65535;
}

static void pairing_key(char *key, size_t capacity, int slot, const char *suffix)
{
    (void)snprintf(key, capacity, "feed%d_%s", slot, suffix);
}

static esp_err_t load_pairing(nvs_handle_t nvs, int slot, app_feed_pairing_t *pairing)
{
    char key[16];
    int32_t port = 8787;
    uint8_t enabled = 0;
    memset(pairing, 0, sizeof(*pairing));
    pairing->port = 8787;

    pairing_key(key, sizeof(key), slot, "enabled");
    (void)nvs_get_u8(nvs, key, &enabled);
    pairing_key(key, sizeof(key), slot, "host");
    (void)read_string(nvs, key, pairing->host, sizeof(pairing->host));
    pairing_key(key, sizeof(key), slot, "port");
    (void)nvs_get_i32(nvs, key, &port);
    pairing->port = (int)port;
    pairing_key(key, sizeof(key), slot, "token");
    (void)read_string(nvs, key, pairing->token, sizeof(pairing->token));
    pairing_key(key, sizeof(key), slot, "ca_pem");
    (void)read_string(nvs, key, pairing->ca_pem, sizeof(pairing->ca_pem));
    pairing->enabled = enabled == 1;
    return ESP_OK;
}

esp_err_t app_settings_load(app_settings_t *settings)
{
    if (settings == NULL) {
        return ESP_ERR_INVALID_ARG;
    }
    memset(settings, 0, sizeof(*settings));
    settings->temp_unit = 'F';
    for (int i = 0; i < APP_MAX_FEED_SLOTS; ++i) {
        settings->pairings[i].port = 8787;
    }

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
    for (int i = 0; i < APP_MAX_FEED_SLOTS; ++i) {
        (void)load_pairing(nvs, i, &settings->pairings[i]);
    }
    uint8_t unit = (uint8_t)settings->temp_unit;
    (void)nvs_get_u8(nvs, "temp_unit", &unit);
    settings->temp_unit = (char)unit;
    (void)nvs_get_i64(nvs, "tls_epoch", &settings->tls_bootstrap_epoch);

    /* Read the prototype keys only as a one-way slot-0 fallback. New USB
     * provisioning writes the feed0_* keys below; slots 1–3 intentionally
     * have no USB transport yet and will be provisioned by a later, explicit
     * pairing/admin flow rather than by extending AGOT_PROVISION. */
    bool has_new_pairing = false;
    for (int i = 0; i < APP_MAX_FEED_SLOTS; ++i) {
        if (pairing_has_credentials(&settings->pairings[i])) {
            has_new_pairing = true;
            break;
        }
    }
    if (!has_new_pairing) {
        app_feed_pairing_t *slot = &settings->pairings[0];
        char legacy_host[APP_HOST_MAX] = {0};
        char legacy_token[APP_TOKEN_MAX] = {0};
        char legacy_ca[APP_CA_PEM_MAX] = {0};
        int32_t legacy_port = 8787;
        (void)read_string(nvs, "host", legacy_host, sizeof(legacy_host));
        (void)nvs_get_i32(nvs, "port", &legacy_port);
        (void)read_string(nvs, "token", legacy_token, sizeof(legacy_token));
        (void)read_string(nvs, "ca_pem", legacy_ca, sizeof(legacy_ca));
        if (legacy_host[0] != '\0' && legacy_token[0] != '\0' && legacy_ca[0] != '\0') {
            snprintf(slot->host, sizeof(slot->host), "%s", legacy_host);
            snprintf(slot->token, sizeof(slot->token), "%s", legacy_token);
            snprintf(slot->ca_pem, sizeof(slot->ca_pem), "%s", legacy_ca);
            slot->port = (int)legacy_port;
            slot->enabled = true;
        }
        secure_clear(legacy_token, sizeof(legacy_token));
        secure_clear(legacy_ca, sizeof(legacy_ca));
    }
    nvs_close(nvs);

    bool has_pairing = false;
    for (int i = 0; i < APP_MAX_FEED_SLOTS; ++i) {
        if (settings->pairings[i].enabled && pairing_has_credentials(&settings->pairings[i])) {
            has_pairing = true;
            break;
        }
    }
    settings->configured = configured == 1 && settings->ssid[0] != '\0' && has_pairing;
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
        (err = nvs_set_u8(nvs, "temp_unit", (uint8_t)settings->temp_unit)) != ESP_OK ||
        (err = nvs_set_i64(nvs, "tls_epoch", settings->tls_bootstrap_epoch)) != ESP_OK) {
        nvs_close(nvs);
        return err;
    }
    for (int i = 0; i < APP_MAX_FEED_SLOTS; ++i) {
        const app_feed_pairing_t *pairing = &settings->pairings[i];
        char key[16];
        pairing_key(key, sizeof(key), i, "enabled");
        if ((err = nvs_set_u8(nvs, key, pairing->enabled ? 1 : 0)) != ESP_OK) {
            nvs_close(nvs);
            return err;
        }
        pairing_key(key, sizeof(key), i, "host");
        if ((err = nvs_set_str(nvs, key, pairing->host)) != ESP_OK) {
            nvs_close(nvs);
            return err;
        }
        pairing_key(key, sizeof(key), i, "port");
        if ((err = nvs_set_i32(nvs, key, pairing->port)) != ESP_OK) {
            nvs_close(nvs);
            return err;
        }
        pairing_key(key, sizeof(key), i, "token");
        if ((err = nvs_set_str(nvs, key, pairing->token)) != ESP_OK) {
            nvs_close(nvs);
            return err;
        }
        pairing_key(key, sizeof(key), i, "ca_pem");
        if ((err = nvs_set_str(nvs, key, pairing->ca_pem)) != ESP_OK) {
            nvs_close(nvs);
            return err;
        }
    }
    if ((err = nvs_set_u8(nvs, "configured", 1)) != ESP_OK ||
        (err = nvs_commit(nvs)) != ESP_OK) {
        nvs_close(nvs);
        return err;
    }
    nvs_close(nvs);
    return ESP_OK;
}

static bool copy_json_string(
    cJSON *root,
    const char *key,
    char *out,
    size_t capacity,
    bool required)
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
    if (root == NULL || !cJSON_IsObject(root)) {
        cJSON_Delete(root);
        return ESP_ERR_INVALID_ARG;
    }
    cJSON *type = cJSON_GetObjectItemCaseSensitive(root, "type");
    cJSON *version = cJSON_GetObjectItemCaseSensitive(root, "version");
    cJSON *port = cJSON_GetObjectItemCaseSensitive(root, "bridgePort");
    cJSON *unit = cJSON_GetObjectItemCaseSensitive(root, "tempUnit");
    cJSON *unix_time = cJSON_GetObjectItemCaseSensitive(root, "unixTime");
    app_feed_pairing_t *slot = &settings->pairings[0];
    bool valid = cJSON_IsString(type) && strcmp(type->valuestring, "provision") == 0 &&
        cJSON_IsNumber(version) && version->valueint == 1 &&
        cJSON_IsNumber(port) && port->valueint > 0 && port->valueint <= 65535 &&
        copy_json_string(root, "ssid", settings->ssid, sizeof(settings->ssid), true) &&
        copy_json_string(root, "password", settings->password, sizeof(settings->password), false) &&
        copy_json_string(root, "bridgeHost", slot->host, sizeof(slot->host), true) &&
        copy_json_string(root, "token", slot->token, sizeof(slot->token), true) &&
        copy_json_string(root, "caPem", slot->ca_pem, sizeof(slot->ca_pem), true);
    if (valid) {
        slot->port = port->valueint;
        slot->enabled = true;
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
    for (int i = 0; i < APP_MAX_FEED_SLOTS; ++i) {
        candidate->pairings[i].port = 8787;
    }
    candidate->temp_unit = 'F';
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
        for (int i = 0; i < APP_MAX_FEED_SLOTS; ++i) {
            candidate->pairings[i].port = 8787;
        }
        candidate->temp_unit = 'F';
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
