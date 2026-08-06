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
#include "esp_system.h"
#include "esp_websocket_client.h"
#include "esp_wifi.h"
#include "freertos/FreeRTOS.h"
#include "freertos/event_groups.h"
#include "freertos/idf_additions.h"
#include "freertos/queue.h"
#include "freertos/semphr.h"
#include "freertos/task.h"
#include "mdns.h"

#define WIFI_CONNECTED_BIT BIT0
#define RSSI_UPDATE_MS 5000
#define WS_RX_MAX 8192
#define FEED_RETRY_DELAY_MS 3000

#define ACTION_CAPABILITY_MAX 16

typedef struct {
    char task_id[APP_TASK_ID_MAX];
    char capability[ACTION_CAPABILITY_MAX];
    int feed_slot;
    uint64_t seen_revision;
} focus_request_t;

typedef struct network_context network_context_t;

typedef struct {
    network_context_t *network;
    int index;
    esp_websocket_client_handle_t websocket;
    char websocket_uri[APP_HOST_MAX + 32];
    char authorization[APP_TOKEN_MAX + 32];
    char receive_buffer[WS_RX_MAX];
    size_t receive_length;
    bool connected;
    bool contribution_valid;
    bool stale;
    bool has_order;
    uint64_t last_generation;
    uint64_t last_revision;
    app_snapshot_t snapshot;
} feed_slot_context_t;

struct network_context {
    const app_settings_t *settings;
    EventGroupHandle_t wifi_events;
    QueueHandle_t focus_queue;
    SemaphoreHandle_t merge_lock;
    /* Four slots embed ~88 KB of receive buffers and per-feed snapshots, which
     * overflows internal DRAM when placed in .bss. The array is allocated from
     * PSRAM in app_network_start (with an internal-RAM fallback).
     * Array indexing through this pointer keeps every access site unchanged. */
    feed_slot_context_t *slots;
    app_snapshot_t merged_snapshot;
    app_ui_event_t snapshot_event;
    uint64_t merge_sequence;
    uint64_t action_sequence;
    bool wifi_connected;
};

static const char *TAG = "pet_network";
static network_context_t s_network;
static atomic_bool s_wall_clock_valid;

static bool pairing_has_credentials(const app_feed_pairing_t *pairing);

static void sntp_sync_callback(struct timeval *time_value)
{
    (void)time_value;
    atomic_store(&s_wall_clock_valid, true);
}

bool app_network_wall_clock_valid(void)
{
    return atomic_load(&s_wall_clock_valid);
}

static int origin_compare(uint64_t left_generation, uint64_t left_revision,
    uint64_t right_generation, uint64_t right_revision)
{
    if (left_generation != right_generation) {
        return left_generation > right_generation ? 1 : -1;
    }
    if (left_revision != right_revision) {
        return left_revision > right_revision ? 1 : -1;
    }
    return 0;
}

static int task_priority(const app_task_t *task)
{
    return task == NULL ? 0 : app_protocol_state_priority(task->state);
}

static bool candidate_is_newer(const app_task_t *candidate, int candidate_slot,
    const app_task_t *current, int current_slot)
{
    int ordering = origin_compare(
        candidate->origin_generation,
        candidate->origin_revision,
        current->origin_generation,
        current->origin_revision);
    return ordering > 0 || (ordering == 0 && candidate_slot < current_slot);
}

/* If the bounded union is full, keep the most urgent records. A lower-priority
 * record is deterministically evicted; equal-priority overflow keeps the first
 * slot/task encountered so a noisy feed cannot reorder the display randomly. */
static int find_worst_task(const app_snapshot_t *snapshot)
{
    int worst = -1;
    for (int i = 0; i < snapshot->task_count; ++i) {
        if (worst < 0 || task_priority(&snapshot->tasks[i]) < task_priority(&snapshot->tasks[worst])) {
            worst = i;
        }
    }
    return worst;
}

static void recompute_merged_metadata(app_snapshot_t *snapshot)
{
    snapshot->aggregate_state = APP_STATE_IDLE;
    snapshot->needs_input_count = 0;
    snapshot->blocked_count = 0;
    snapshot->ready_count = 0;
    snapshot->running_count = 0;
    int aggregate_priority = 0;
    for (int i = 0; i < snapshot->task_count; ++i) {
        const app_task_t *task = &snapshot->tasks[i];
        switch (task->state) {
        case APP_STATE_NEEDS_INPUT:
            snapshot->needs_input_count++;
            break;
        case APP_STATE_BLOCKED:
            snapshot->blocked_count++;
            break;
        case APP_STATE_READY:
            snapshot->ready_count++;
            break;
        case APP_STATE_RUNNING:
            snapshot->running_count++;
            break;
        case APP_STATE_IDLE:
        default:
            break;
        }
        int priority = task_priority(task);
        if (priority > aggregate_priority) {
            aggregate_priority = priority;
            snapshot->aggregate_state = task->state;
        }
    }
}

static void rebuild_merged_snapshot_locked(network_context_t *context)
{
    memset(&context->merged_snapshot, 0, sizeof(context->merged_snapshot));
    context->merged_snapshot.seq = ++context->merge_sequence;
    context->merged_snapshot.aggregate_state = APP_STATE_IDLE;

    for (int slot_index = 0; slot_index < APP_MAX_FEED_SLOTS; ++slot_index) {
        feed_slot_context_t *slot = &context->slots[slot_index];
        if (!slot->contribution_valid) {
            continue;
        }
        for (int task_index = 0; task_index < slot->snapshot.task_count; ++task_index) {
            app_task_t candidate = slot->snapshot.tasks[task_index];
            candidate.feed_slot = (int8_t)slot_index;
            candidate.origin_generation = slot->snapshot.origin_generation;
            candidate.origin_revision = slot->snapshot.origin_revision;

            int existing = -1;
            for (int i = 0; i < context->merged_snapshot.task_count; ++i) {
                if (strcmp(context->merged_snapshot.tasks[i].id, candidate.id) == 0) {
                    existing = i;
                    break;
                }
            }
            if (existing >= 0) {
                int current_slot = context->merged_snapshot.tasks[existing].feed_slot;
                if (candidate_is_newer(
                        &candidate,
                        slot_index,
                        &context->merged_snapshot.tasks[existing],
                        current_slot)) {
                    context->merged_snapshot.tasks[existing] = candidate;
                }
                continue;
            }

            if (context->merged_snapshot.task_count < APP_MAX_TASKS) {
                context->merged_snapshot.tasks[context->merged_snapshot.task_count++] = candidate;
                continue;
            }
            int worst = find_worst_task(&context->merged_snapshot);
            if (worst >= 0 && task_priority(&candidate) > task_priority(&context->merged_snapshot.tasks[worst])) {
                context->merged_snapshot.tasks[worst] = candidate;
            }
        }
    }
    recompute_merged_metadata(&context->merged_snapshot);
}

static void post_merged_snapshot_locked(network_context_t *context)
{
    context->snapshot_event.type = APP_UI_EVENT_SNAPSHOT;
    context->snapshot_event.data.snapshot = context->merged_snapshot;
    app_ui_post(&context->snapshot_event);
}

static int connected_feed_count(network_context_t *context)
{
    int connected = 0;
    xSemaphoreTake(context->merge_lock, portMAX_DELAY);
    for (int i = 0; i < APP_MAX_FEED_SLOTS; ++i) {
        connected += context->slots[i].connected ? 1 : 0;
    }
    xSemaphoreGive(context->merge_lock);
    return connected;
}

static void post_network_state(network_context_t *context)
{
    int connected = connected_feed_count(context);
    /* The event union embeds a full snapshot; keep it off the task stack.
     * post_network_state runs only on the network task. */
    static app_ui_event_t event;
    event = (app_ui_event_t){
        .type = APP_UI_EVENT_NETWORK,
        .data.network = {
            .websocket_connected = connected > 0,
            .wifi_connected = context->wifi_connected,
            .rssi = -127,
            .connected_feeds = connected,
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
        post_network_state(context);
        (void)esp_wifi_connect();
    } else if (event_base == IP_EVENT && event_id == IP_EVENT_STA_GOT_IP) {
        context->wifi_connected = true;
        xEventGroupSetBits(context->wifi_events, WIFI_CONNECTED_BIT);
        post_network_state(context);
    }
}

static void handle_snapshot(feed_slot_context_t *slot, const char *json, size_t length)
{
    esp_err_t err = app_protocol_parse_snapshot(json, length, &slot->snapshot);
    if (err != ESP_OK) {
        /* Action results and unknown frames are intentionally ignored. No raw
         * payload, bearer credential, or other private value is logged. */
        if (err != ESP_ERR_NOT_SUPPORTED) {
            ESP_LOGW(TAG, "ignored invalid feed snapshot on slot %d", slot->index);
        }
        return;
    }

    network_context_t *context = slot->network;
    xSemaphoreTake(context->merge_lock, portMAX_DELAY);
    const uint64_t generation = slot->snapshot.origin_generation;
    const uint64_t revision = slot->snapshot.origin_revision;
    bool newer = !slot->has_order || origin_compare(
        generation,
        revision,
        slot->last_generation,
        slot->last_revision) > 0;
    bool equal = slot->has_order && origin_compare(
        generation,
        revision,
        slot->last_generation,
        slot->last_revision) == 0;
    if ((slot->stale && !newer) || (!newer && !equal)) {
        /* A regression drops this complete contribution. A later strictly
         * newer generation/revision is the only accepted resynchronization. */
        slot->stale = true;
        slot->contribution_valid = false;
        rebuild_merged_snapshot_locked(context);
        post_merged_snapshot_locked(context);
        xSemaphoreGive(context->merge_lock);
        return;
    }
    slot->last_generation = generation;
    slot->last_revision = revision;
    slot->has_order = true;
    slot->stale = false;
    slot->contribution_valid = true;
    rebuild_merged_snapshot_locked(context);
    post_merged_snapshot_locked(context);
    xSemaphoreGive(context->merge_lock);
}

static void websocket_event_handler(
    void *handler_args,
    esp_event_base_t event_base,
    int32_t event_id,
    void *event_data)
{
    (void)event_base;
    feed_slot_context_t *slot = handler_args;
    if (slot == NULL || slot->network == NULL) {
        return;
    }
    network_context_t *context = slot->network;
    esp_websocket_event_data_t *event = event_data;

    if (event_id == WEBSOCKET_EVENT_CONNECTED) {
        xSemaphoreTake(context->merge_lock, portMAX_DELAY);
        slot->connected = true;
        slot->receive_length = 0;
        xSemaphoreGive(context->merge_lock);
        post_network_state(context);
        return;
    }
    if (event_id == WEBSOCKET_EVENT_DISCONNECTED ||
        event_id == WEBSOCKET_EVENT_CLOSED ||
        event_id == WEBSOCKET_EVENT_ERROR) {
        bool had_contribution = false;
        slot->receive_length = 0;
        xSemaphoreTake(context->merge_lock, portMAX_DELAY);
        slot->connected = false;
        had_contribution = slot->contribution_valid;
        slot->contribution_valid = false;
        if (had_contribution) {
            rebuild_merged_snapshot_locked(context);
            post_merged_snapshot_locked(context);
        }
        xSemaphoreGive(context->merge_lock);
        post_network_state(context);
        return;
    }
    if (event_id != WEBSOCKET_EVENT_DATA || event == NULL ||
        (event->op_code != 0x01 && event->op_code != 0x00)) {
        return;
    }
    if (event->payload_len <= 0 || event->payload_len >= WS_RX_MAX ||
        event->payload_offset < 0 || event->data_len < 0 ||
        event->payload_offset > event->payload_len ||
        event->data_len > event->payload_len - event->payload_offset ||
        event->data_ptr == NULL) {
        slot->receive_length = 0;
        return;
    }
    if (event->payload_offset == 0) {
        slot->receive_length = 0;
    }
    if ((size_t)event->payload_offset != slot->receive_length ||
        (size_t)event->data_len > sizeof(slot->receive_buffer) - slot->receive_length - 1) {
        slot->receive_length = 0;
        return;
    }
    memcpy(slot->receive_buffer + slot->receive_length, event->data_ptr, event->data_len);
    slot->receive_length += (size_t)event->data_len;
    if (event->fin && slot->receive_length == (size_t)event->payload_len) {
        slot->receive_buffer[slot->receive_length] = '\0';
        handle_snapshot(slot, slot->receive_buffer, slot->receive_length);
        slot->receive_length = 0;
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
        ESP_LOGI(TAG, "bridge discovered for feed slot 0 at %s:%d", host, *port);
        break;
    }
    mdns_query_results_free(results);
}

static esp_err_t start_websocket(network_context_t *context, int index)
{
    feed_slot_context_t *slot = &context->slots[index];
    const app_feed_pairing_t *pairing = &context->settings->pairings[index];
    char host[APP_HOST_MAX];
    int port = pairing->port;
    snprintf(host, sizeof(host), "%s", pairing->host);
    if (index == 0) {
        discover_bridge(host, sizeof(host), &port);
    }

    int uri_length = snprintf(
        slot->websocket_uri,
        sizeof(slot->websocket_uri),
        "wss://%s:%d/feed/v1",
        host,
        port);
    int auth_length = snprintf(
        slot->authorization,
        sizeof(slot->authorization),
        "Authorization: Bearer %s\r\n",
        pairing->token);
    if (uri_length <= 0 || uri_length >= (int)sizeof(slot->websocket_uri) ||
        auth_length <= 0 || auth_length >= (int)sizeof(slot->authorization)) {
        return ESP_ERR_INVALID_SIZE;
    }

    const esp_websocket_client_config_t websocket_config = {
        .uri = slot->websocket_uri,
        .headers = slot->authorization,
        .cert_pem = pairing->ca_pem,
        /* Bonjour's numeric address avoids unreliable .local getaddrinfo on
         * some ESP-IDF/LwIP combinations. Continue checking the pinned
         * certificate against the provisioned feed identity. */
        .cert_common_name = pairing->host,
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
    slot->websocket = esp_websocket_client_init(&websocket_config);
    if (slot->websocket == NULL) {
        return ESP_ERR_NO_MEM;
    }
    esp_err_t err = esp_websocket_register_events(
        slot->websocket,
        WEBSOCKET_EVENT_ANY,
        websocket_event_handler,
        slot);
    if (err == ESP_OK) {
        err = esp_websocket_client_start(slot->websocket);
    }
    if (err != ESP_OK) {
        (void)esp_websocket_client_destroy(slot->websocket);
        slot->websocket = NULL;
    }
    return err;
}

static void make_action_id(network_context_t *context, char *output, size_t capacity)
{
    uint32_t random_a = esp_random();
    uint32_t random_b = esp_random();
    uint64_t counter = ++context->action_sequence;
    (void)snprintf(
        output,
        capacity,
        "agot-%08lx-%08lx-%llu",
        (unsigned long)random_a,
        (unsigned long)random_b,
        (unsigned long long)counter);
}

static void send_focus(network_context_t *context, const focus_request_t *request)
{
    if (request == NULL || request->feed_slot < 0 || request->feed_slot >= APP_MAX_FEED_SLOTS) {
        return;
    }
    feed_slot_context_t *slot = &context->slots[request->feed_slot];
    if (!slot->connected || slot->websocket == NULL) {
        return;
    }
    char action_id[80];
    make_action_id(context, action_id, sizeof(action_id));
    char json[APP_TASK_ID_MAX + sizeof(action_id) + sizeof(request->capability) + 160];
    int length = snprintf(
        json,
        sizeof(json),
        "{\"schema\":\"agentagotchi.feed.v1\",\"type\":\"action\","
        "\"actionId\":\"%s\",\"capability\":\"%s\","
        "\"taskPresenceId\":\"%s\",\"seenRevision\":%llu}",
        action_id,
        request->capability,
        request->task_id,
        (unsigned long long)request->seen_revision);
    if (length > 0 && length < (int)sizeof(json)) {
        /* This is deliberately one-shot. The protocol never queues an action
         * across reconnects or silently retries it on another feed. */
        (void)esp_websocket_client_send_text(
            slot->websocket,
            json,
            length,
            pdMS_TO_TICKS(1000));
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

    TickType_t last_start[APP_MAX_FEED_SLOTS] = {0};
    TickType_t last_rssi = 0;
    focus_request_t request;
    while (true) {
        TickType_t now = xTaskGetTickCount();
        for (int i = 0; i < APP_MAX_FEED_SLOTS; ++i) {
            const app_feed_pairing_t *pairing = &context->settings->pairings[i];
            if (!pairing->enabled || !pairing_has_credentials(pairing) ||
                context->slots[i].websocket != NULL ||
                (last_start[i] != 0 && now - last_start[i] < pdMS_TO_TICKS(FEED_RETRY_DELAY_MS))) {
                continue;
            }
            last_start[i] = now;
            esp_err_t err = start_websocket(context, i);
            if (err != ESP_OK) {
                ESP_LOGW(TAG, "feed slot %d initialization failed; retrying", i);
            }
        }

        if (xQueueReceive(context->focus_queue, &request, pdMS_TO_TICKS(250)) == pdTRUE) {
            send_focus(context, &request);
        }
        now = xTaskGetTickCount();
        if (now - last_rssi >= pdMS_TO_TICKS(RSSI_UPDATE_MS)) {
            wifi_ap_record_t access_point = {0};
            int rssi = -127;
            if ((xEventGroupGetBits(context->wifi_events) & WIFI_CONNECTED_BIT) != 0 &&
                esp_wifi_sta_get_ap_info(&access_point) == ESP_OK) {
                rssi = access_point.rssi;
            }
            /* Same union-on-stack hazard; network task only. */
            static app_ui_event_t event;
            event = (app_ui_event_t){
                .type = APP_UI_EVENT_NETWORK,
                .data.network = {
                    .websocket_connected = false,
                    .wifi_connected = context->wifi_connected,
                    .rssi = rssi,
                    .connected_feeds = 0,
                },
            };
            int connected = connected_feed_count(context);
            event.data.network.websocket_connected = connected > 0;
            event.data.network.connected_feeds = connected;
            app_ui_post(&event);
            last_rssi = now;
        }
    }
}

static bool pairing_has_credentials(const app_feed_pairing_t *pairing)
{
    return pairing != NULL && pairing->host[0] != '\0' && pairing->token[0] != '\0' &&
        pairing->ca_pem[0] != '\0' && pairing->port > 0 && pairing->port <= 65535;
}

esp_err_t app_network_start(const app_settings_t *settings)
{
    if (settings == NULL || !settings->configured) {
        return ESP_ERR_INVALID_ARG;
    }
    memset(&s_network, 0, sizeof(s_network));
    atomic_store(&s_wall_clock_valid, false);
    s_network.settings = settings;
    s_network.slots = heap_caps_malloc(
        APP_MAX_FEED_SLOTS * sizeof(feed_slot_context_t),
        MALLOC_CAP_SPIRAM | MALLOC_CAP_8BIT);
    if (s_network.slots == NULL) {
        s_network.slots = heap_caps_malloc(
            APP_MAX_FEED_SLOTS * sizeof(feed_slot_context_t),
            MALLOC_CAP_INTERNAL | MALLOC_CAP_8BIT);
    }
    if (s_network.slots == NULL) {
        return ESP_ERR_NO_MEM;
    }
    memset(s_network.slots, 0, APP_MAX_FEED_SLOTS * sizeof(feed_slot_context_t));
    s_network.wifi_events = xEventGroupCreate();
    s_network.focus_queue = xQueueCreate(8, sizeof(focus_request_t));
    s_network.merge_lock = xSemaphoreCreateMutex();
    if (s_network.wifi_events == NULL || s_network.focus_queue == NULL ||
        s_network.merge_lock == NULL) {
        return ESP_ERR_NO_MEM;
    }
    for (int i = 0; i < APP_MAX_FEED_SLOTS; ++i) {
        s_network.slots[i].network = &s_network;
        s_network.slots[i].index = i;
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

    /* Task stacks must be internal RAM (Xtensa context switch). */
    if (xTaskCreatePinnedToCore(
            network_task,
            "pet_network",
            8192,
            &s_network,
            5,
            NULL,
            0) != pdPASS) {
        return ESP_ERR_NO_MEM;
    }
    return ESP_OK;
}

esp_err_t app_network_request_focus(
    const char *task_id,
    int feed_slot,
    uint64_t seen_revision)
{
    if (task_id == NULL || task_id[0] == '\0' ||
        feed_slot < 0 || feed_slot >= APP_MAX_FEED_SLOTS ||
        s_network.focus_queue == NULL) {
        return ESP_ERR_INVALID_STATE;
    }
    focus_request_t request = {
        .feed_slot = feed_slot,
        .seen_revision = seen_revision,
    };
    snprintf(request.task_id, sizeof(request.task_id), "%s", task_id);
    snprintf(request.capability, sizeof(request.capability), "%s", "focus");
    return xQueueSend(s_network.focus_queue, &request, 0) == pdTRUE
        ? ESP_OK
        : ESP_ERR_TIMEOUT;
}

esp_err_t app_network_request_dismiss(
    const char *task_id,
    int feed_slot,
    uint64_t seen_revision,
    app_dismiss_mode_t mode)
{
    if (task_id == NULL || task_id[0] == '\0' ||
        feed_slot < 0 || feed_slot >= APP_MAX_FEED_SLOTS ||
        s_network.focus_queue == NULL) {
        return ESP_ERR_INVALID_STATE;
    }
    /* Dismissal actions are Edge-global controls (docs/PROTOCOL.md): the same
     * one-shot action frame as focus, capability "acknowledge" or "snooze".
     * The owning Edge enforces target-state rules (terminal vs needs_input)
     * and acknowledges only on exact success. */
    focus_request_t request = {
        .feed_slot = feed_slot,
        .seen_revision = seen_revision,
    };
    snprintf(request.task_id, sizeof(request.task_id), "%s", task_id);
    snprintf(
        request.capability,
        sizeof(request.capability),
        "%s",
        mode == APP_DISMISS_ACKNOWLEDGE ? "acknowledge" : "snooze");
    return xQueueSend(s_network.focus_queue, &request, 0) == pdTRUE
        ? ESP_OK
        : ESP_ERR_TIMEOUT;
}
