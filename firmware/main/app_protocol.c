#include "app_protocol.h"

#include <string.h>

#include "cJSON.h"

static app_agent_state_t parse_state(const char *state)
{
    if (state == NULL) {
        return APP_STATE_IDLE;
    }
    if (strcmp(state, "running") == 0) {
        return APP_STATE_RUNNING;
    }
    if (strcmp(state, "needs_input") == 0) {
        return APP_STATE_NEEDS_INPUT;
    }
    if (strcmp(state, "ready") == 0) {
        return APP_STATE_READY;
    }
    if (strcmp(state, "blocked") == 0) {
        return APP_STATE_BLOCKED;
    }
    return APP_STATE_IDLE;
}

static void copy_string(char *destination, size_t capacity, cJSON *value)
{
    destination[0] = '\0';
    if (!cJSON_IsString(value) || value->valuestring == NULL || capacity == 0) {
        return;
    }
    size_t length = strlen(value->valuestring);
    if (length >= capacity) {
        length = capacity - 1;
    }
    memcpy(destination, value->valuestring, length);
    destination[length] = '\0';
}

esp_err_t app_protocol_parse_snapshot(const char *json, size_t length, app_snapshot_t *snapshot)
{
    if (json == NULL || snapshot == NULL || length == 0) {
        return ESP_ERR_INVALID_ARG;
    }
    cJSON *root = cJSON_ParseWithLength(json, length);
    if (root == NULL) {
        return ESP_ERR_INVALID_ARG;
    }
    cJSON *type = cJSON_GetObjectItemCaseSensitive(root, "type");
    cJSON *version = cJSON_GetObjectItemCaseSensitive(root, "version");
    if (!cJSON_IsString(type) || strcmp(type->valuestring, "snapshot") != 0 ||
        !cJSON_IsNumber(version) || version->valueint != 1) {
        cJSON_Delete(root);
        return ESP_ERR_NOT_SUPPORTED;
    }

    memset(snapshot, 0, sizeof(*snapshot));
    cJSON *seq = cJSON_GetObjectItemCaseSensitive(root, "seq");
    if (cJSON_IsNumber(seq) && seq->valuedouble >= 0) {
        snapshot->seq = (uint64_t)seq->valuedouble;
    }
    cJSON *aggregate = cJSON_GetObjectItemCaseSensitive(root, "aggregateState");
    snapshot->aggregate_state = parse_state(cJSON_IsString(aggregate) ? aggregate->valuestring : NULL);

    cJSON *counts = cJSON_GetObjectItemCaseSensitive(root, "counts");
    if (cJSON_IsObject(counts)) {
        cJSON *v = cJSON_GetObjectItemCaseSensitive(counts, "needsInput");
        snapshot->needs_input_count = cJSON_IsNumber(v) ? v->valueint : 0;
        v = cJSON_GetObjectItemCaseSensitive(counts, "blocked");
        snapshot->blocked_count = cJSON_IsNumber(v) ? v->valueint : 0;
        v = cJSON_GetObjectItemCaseSensitive(counts, "ready");
        snapshot->ready_count = cJSON_IsNumber(v) ? v->valueint : 0;
        v = cJSON_GetObjectItemCaseSensitive(counts, "running");
        snapshot->running_count = cJSON_IsNumber(v) ? v->valueint : 0;
    }

    cJSON *tasks = cJSON_GetObjectItemCaseSensitive(root, "tasks");
    cJSON *item = NULL;
    cJSON_ArrayForEach(item, tasks) {
        if (snapshot->task_count >= APP_MAX_TASKS || !cJSON_IsObject(item)) {
            break;
        }
        app_task_t *task = &snapshot->tasks[snapshot->task_count];
        copy_string(task->id, sizeof(task->id), cJSON_GetObjectItemCaseSensitive(item, "id"));
        copy_string(task->title, sizeof(task->title), cJSON_GetObjectItemCaseSensitive(item, "title"));
        copy_string(task->reason, sizeof(task->reason), cJSON_GetObjectItemCaseSensitive(item, "reason"));
        cJSON *state = cJSON_GetObjectItemCaseSensitive(item, "state");
        task->state = parse_state(cJSON_IsString(state) ? state->valuestring : NULL);
        cJSON *agents = cJSON_GetObjectItemCaseSensitive(item, "subagentCount");
        task->subagent_count = cJSON_IsNumber(agents) ? agents->valueint : 0;
        if (task->id[0] != '\0') {
            snapshot->task_count++;
        }
    }
    cJSON_Delete(root);
    return ESP_OK;
}

const char *app_protocol_state_name(app_agent_state_t state)
{
    switch (state) {
    case APP_STATE_RUNNING:
        return "RUNNING";
    case APP_STATE_NEEDS_INPUT:
        return "NEEDS INPUT";
    case APP_STATE_READY:
        return "READY";
    case APP_STATE_BLOCKED:
        return "BLOCKED";
    default:
        return "IDLE";
    }
}
