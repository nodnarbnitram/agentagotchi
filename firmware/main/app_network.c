#include "app_network.h"

#include <stdio.h>
#include <stdatomic.h>
#include <string.h>

#include "app_protocol.h"
#include "app_state.h"
#include "esp_check.h"
#include "esp_event.h"
#include "esp_heap_caps.h"
#include "esp_log.h"
#include "esp_netif.h"
#include "esp_netif_ip_addr.h"
#include "esp_netif_sntp.h"
#include "esp_websocket_client.h"
#include "esp_wifi.h"
#include "freertos/FreeRTOS.h"
#include "freertos/event_groups.h"
#include "freertos/idf_additions.h"
#include "freertos/queue.h"
#include "freertos/task.h"
#include "mdns.h"

#define WIFI_CONNECTED_BIT BIT0
#define WIFI_RETRY_DELAY_MS 3000
#define RSSI_UPDATE_MS 5000
#define WS_RX_MAX 8192

typedef struct {
    char task_id[APP_TASK_ID_MAX];
    uint64_t seen_seq;
} focus_request_t;

typedef struct {
    app_settings_t settings;
    EventGroupHandle_t wifi_events;
    QueueHandle_t focus_queue;
    esp_websocket_client_handle_t websocket;
    char websocket_uri[APP_HOST_MAX + 32];
    char authorization[APP_TOKEN_MAX + 32];
    char receive_buffer[WS_RX_MAX];
    size_t receive_length;
    bool wifi_connected;
    bool websocket_connected;
} network_context_t;

static const char *TAG = "pet_network";
static network_context_t s_network;
static atomic_bool s_wall_clock_valid;

static void sntp_sync_callback(struct timeval *time_value)
{
    (void)time_value;
    atomic_store(&s_wall_clock_valid, true);
}

bool app_network_wall_clock_valid(void)
{
    return atomic_load(&s_wall_clock_valid);
}

static void post_network_state(
    bool websocket_connected,
    bool wifi_connected,
    int rssi)
{
    app_ui_event_t event = {
        .type = APP_UI_EVENT_NETWORK,
        .data.network = {
            .websocket_connected = websocket_connected,
            .wifi_connected = wifi_connected,
            .rssi = rssi,
        },
    };
    app_ui_post(&event);
}

static void wifi_event_handler(
    void *argument,
    esp_event_base_t event_base,
    int32_t event_id,
    void *event_data)
{
    (void)event_data;
    network_context_t *context = argument;
    if (event_base == WIFI_EVENT && event_id == WIFI_EVENT_STA_START) {
        (void)esp_wifi_connect();
    } else if (event_base == WIFI_EVENT && event_id == WIFI_EVENT_STA_DISCONNECTED) {
        context->wifi_connected = false;
        xEventGroupClearBits(context->wifi_events, WIFI_CONNECTED_BIT);
        post_network_state(context->websocket_connected, false, -127);
        (void)esp_wifi_connect();
    } else if (event_base == IP_EVENT && event_id == IP_EVENT_STA_GOT_IP) {
        context->wifi_connected = true;
        xEventGroupSetBits(context->wifi_events, WIFI_CONNECTED_BIT);
        post_network_state(context->websocket_connected, true, -127);
    }
}

static void submit_snapshot(const char *json, size_t length)
{
    app_snapshot_t snapshot;
    if (app_protocol_parse_snapshot(json, length, &snapshot) != ESP_OK) {
        ESP_LOGW(TAG, "ignored invalid snapshot");
        return;
    }
    app_ui_event_t event = {
        .type = APP_UI_EVENT_SNAPSHOT,
        .data.snapshot = snapshot,
    };
    app_ui_post(&event);
}

static void websocket_event_handler(
    void *handler_args,
    esp_event_base_t event_base,
    int32_t event_id,
    void *event_data)
{
    (void)event_base;
    network_context_t *context = handler_args;
    esp_websocket_event_data_t *event = event_data;

    if (event_id == WEBSOCKET_EVENT_CONNECTED) {
        context->websocket_connected = true;
        post_network_state(true, context->wifi_connected, -127);
        return;
    }
    if (event_id == WEBSOCKET_EVENT_DISCONNECTED ||
        event_id == WEBSOCKET_EVENT_CLOSED ||
        event_id == WEBSOCKET_EVENT_ERROR) {
        context->websocket_connected = false;
        context->receive_length = 0;
        post_network_state(false, context->wifi_connected, -127);
        return;
    }
    if (event_id != WEBSOCKET_EVENT_DATA || event == NULL ||
        (event->op_code != 0x01 && event->op_code != 0x00)) {
        return;
    }
    if (event->payload_len <= 0 || event->payload_len >= WS_RX_MAX ||
        event->payload_offset < 0 || event->data_len < 0 ||
        event->payload_offset + event->data_len > event->payload_len) {
        context->receive_length = 0;
        return;
    }
    if (event->payload_offset == 0) {
        context->receive_length = 0;
    }
    if ((size_t)event->payload_offset != context->receive_length) {
        context->receive_length = 0;
        return;
    }
    memcpy(context->receive_buffer + event->payload_offset, event->data_ptr, event->data_len);
    context->receive_length += (size_t)event->data_len;
    if (event->fin && context->receive_length == (size_t)event->payload_len) {
        context->receive_buffer[context->receive_length] = '\0';
        submit_snapshot(context->receive_buffer, context->receive_length);
        context->receive_length = 0;
    }
}

static bool discovered_ipv4(
    const mdns_result_t *result,
    char *host,
    size_t host_capacity)
{
    for (const mdns_ip_addr_t *address = result->addr;
         address != NULL;
         address = address->next) {
        if (address->addr.type != ESP_IPADDR_TYPE_V4) {
            continue;
        }
        int length = snprintf(
            host,
            host_capacity,
            IPSTR,
            IP2STR(&address->addr.u_addr.ip4));
        return length > 0 && length < (int)host_capacity;
    }
    return false;
}

static void discover_bridge(char *host, size_t host_capacity, int *port)
{
    mdns_result_t *results = NULL;
    if (mdns_init() != ESP_OK ||
        mdns_query_ptr("_agentagotchi", "_tcp", 2500, 4, &results) != ESP_OK) {
        return;
    }
    for (mdns_result_t *result = results; result != NULL; result = result->next) {
        if (result->hostname == NULL || result->hostname[0] == '\0' || result->port == 0) {
            continue;
        }
        if (!discovered_ipv4(result, host, host_capacity)) {
            if (strchr(result->hostname, '.') == NULL) {
                snprintf(host, host_capacity, "%s.local", result->hostname);
            } else {
                snprintf(host, host_capacity, "%s", result->hostname);
            }
        }
        *port = result->port;
        ESP_LOGI(TAG, "bridge discovered at %s:%d", host, *port);
        break;
    }
    mdns_query_results_free(results);
}

static esp_err_t start_websocket(network_context_t *context)
{
    char host[APP_HOST_MAX];
    int port = context->settings.bridge_port;
    snprintf(host, sizeof(host), "%s", context->settings.bridge_host);
    discover_bridge(host, sizeof(host), &port);

    int uri_length = snprintf(
        context->websocket_uri,
        sizeof(context->websocket_uri),
        "wss://%s:%d/ws",
        host,
        port);
    int auth_length = snprintf(
        context->authorization,
        sizeof(context->authorization),
        "Authorization: Bearer %s\r\n",
        context->settings.token);
    if (uri_length <= 0 || uri_length >= (int)sizeof(context->websocket_uri) ||
        auth_length <= 0 || auth_length >= (int)sizeof(context->authorization)) {
        return ESP_ERR_INVALID_SIZE;
    }

    const esp_websocket_client_config_t websocket_config = {
        .uri = context->websocket_uri,
        .headers = context->authorization,
        .cert_pem = context->settings.ca_pem,
        /* Bonjour's numeric address avoids unreliable .local getaddrinfo on
         * some ESP-IDF/LwIP combinations. Continue checking the pinned
         * certificate against the provisioned bridge identity. */
        .cert_common_name = context->settings.bridge_host,
        .skip_cert_common_name_check = false,
        .buffer_size = 4096,
        .network_timeout_ms = 10000,
        .reconnect_timeout_ms = 3000,
        .ping_interval_sec = 15,
        .pingpong_timeout_sec = 10,
        .keep_alive_enable = true,
        .keep_alive_idle = 15,
        .keep_alive_interval = 5,
        .keep_alive_count = 3,
        .task_stack = 7168,
        .task_prio = 5,
    };
    context->websocket = esp_websocket_client_init(&websocket_config);
    if (context->websocket == NULL) {
        return ESP_ERR_NO_MEM;
    }
    esp_err_t err = esp_websocket_register_events(
        context->websocket,
        WEBSOCKET_EVENT_ANY,
        websocket_event_handler,
        context);
    if (err == ESP_OK) {
        err = esp_websocket_client_start(context->websocket);
    }
    if (err != ESP_OK) {
        (void)esp_websocket_client_destroy(context->websocket);
        context->websocket = NULL;
    }
    return err;
}

static void send_focus(network_context_t *context, const focus_request_t *request)
{
    if (!context->websocket_connected || context->websocket == NULL) {
        return;
    }
    char json[APP_TASK_ID_MAX + 96];
    int length = snprintf(
        json,
        sizeof(json),
        "{\"type\":\"focus\",\"version\":1,\"taskId\":\"%s\",\"seenSeq\":%llu}",
        request->task_id,
        (unsigned long long)request->seen_seq);
    if (length > 0 && length < (int)sizeof(json)) {
        (void)esp_websocket_client_send_text(
            context->websocket, json, length, pdMS_TO_TICKS(1000));
    }
}

static void network_task(void *argument)
{
    network_context_t *context = argument;
    xEventGroupWaitBits(
        context->wifi_events,
        WIFI_CONNECTED_BIT,
        pdFALSE,
        pdTRUE,
        portMAX_DELAY);
    esp_sntp_config_t time_config =
        ESP_NETIF_SNTP_DEFAULT_CONFIG("pool.ntp.org");
    time_config.sync_cb = sntp_sync_callback;
    esp_err_t time_err = esp_netif_sntp_init(&time_config);
    if (time_err != ESP_OK && time_err != ESP_ERR_INVALID_STATE) {
        ESP_LOGW(TAG, "SNTP initialization failed: %s", esp_err_to_name(time_err));
    }
    while (context->websocket == NULL) {
        if (start_websocket(context) == ESP_OK) {
            break;
        }
        ESP_LOGW(TAG, "websocket initialization failed; retrying");
        vTaskDelay(pdMS_TO_TICKS(WIFI_RETRY_DELAY_MS));
    }

    focus_request_t request;
    TickType_t last_rssi = 0;
    while (true) {
        if (xQueueReceive(context->focus_queue, &request, pdMS_TO_TICKS(250)) == pdTRUE) {
            send_focus(context, &request);
        }
        TickType_t now = xTaskGetTickCount();
        if (now - last_rssi >= pdMS_TO_TICKS(RSSI_UPDATE_MS)) {
            wifi_ap_record_t access_point = {0};
            if ((xEventGroupGetBits(context->wifi_events) & WIFI_CONNECTED_BIT) != 0 &&
                esp_wifi_sta_get_ap_info(&access_point) == ESP_OK) {
                post_network_state(
                    context->websocket_connected,
                    context->wifi_connected,
                    access_point.rssi);
            }
            last_rssi = now;
        }
    }
}

esp_err_t app_network_start(const app_settings_t *settings)
{
    if (settings == NULL || !settings->configured) {
        return ESP_ERR_INVALID_ARG;
    }
    memset(&s_network, 0, sizeof(s_network));
    atomic_store(&s_wall_clock_valid, false);
    s_network.settings = *settings;
    s_network.wifi_events = xEventGroupCreate();
    s_network.focus_queue = xQueueCreate(4, sizeof(focus_request_t));
    if (s_network.wifi_events == NULL || s_network.focus_queue == NULL) {
        return ESP_ERR_NO_MEM;
    }

    ESP_RETURN_ON_ERROR(esp_netif_init(), TAG, "init netif");
    esp_err_t event_loop_err = esp_event_loop_create_default();
    if (event_loop_err != ESP_OK && event_loop_err != ESP_ERR_INVALID_STATE) {
        return event_loop_err;
    }
    if (esp_netif_create_default_wifi_sta() == NULL) {
        return ESP_ERR_NO_MEM;
    }
    wifi_init_config_t wifi_init = WIFI_INIT_CONFIG_DEFAULT();
    ESP_RETURN_ON_ERROR(esp_wifi_init(&wifi_init), TAG, "init Wi-Fi");
    ESP_RETURN_ON_ERROR(
        esp_event_handler_register(WIFI_EVENT, ESP_EVENT_ANY_ID, wifi_event_handler, &s_network),
        TAG,
        "register Wi-Fi events");
    ESP_RETURN_ON_ERROR(
        esp_event_handler_register(IP_EVENT, IP_EVENT_STA_GOT_IP, wifi_event_handler, &s_network),
        TAG,
        "register IP events");

    wifi_config_t wifi_config = {0};
    memcpy(
        wifi_config.sta.ssid,
        settings->ssid,
        strnlen(settings->ssid, sizeof(wifi_config.sta.ssid)));
    memcpy(
        wifi_config.sta.password,
        settings->password,
        strnlen(settings->password, sizeof(wifi_config.sta.password)));
    wifi_config.sta.threshold.authmode = WIFI_AUTH_WPA2_PSK;
    wifi_config.sta.sae_pwe_h2e = WPA3_SAE_PWE_BOTH;
    ESP_RETURN_ON_ERROR(esp_wifi_set_mode(WIFI_MODE_STA), TAG, "set Wi-Fi mode");
    ESP_RETURN_ON_ERROR(esp_wifi_set_config(WIFI_IF_STA, &wifi_config), TAG, "set Wi-Fi config");
    ESP_RETURN_ON_ERROR(esp_wifi_start(), TAG, "start Wi-Fi");

    if (xTaskCreatePinnedToCoreWithCaps(
            network_task,
            "pet_network",
            8192,
            &s_network,
            5,
            NULL,
            0,
            MALLOC_CAP_SPIRAM | MALLOC_CAP_8BIT) != pdPASS) {
        return ESP_ERR_NO_MEM;
    }
    return ESP_OK;
}

esp_err_t app_network_request_focus(const char *task_id, uint64_t seen_seq)
{
    if (task_id == NULL || task_id[0] == '\0' || s_network.focus_queue == NULL) {
        return ESP_ERR_INVALID_STATE;
    }
    focus_request_t request = {
        .seen_seq = seen_seq,
    };
    snprintf(request.task_id, sizeof(request.task_id), "%s", task_id);
    return xQueueSend(s_network.focus_queue, &request, 0) == pdTRUE
        ? ESP_OK
        : ESP_ERR_TIMEOUT;
}
