#include "app_protocol.h"

#include <ctype.h>
#include <math.h>
#include <stdint.h>
#include <stdlib.h>
#include <string.h>

#include "cJSON.h"

static bool parse_state(const char *value, app_agent_state_t *state)
{
    if (value == NULL || state == NULL) {
        return false;
    }
    if (strcmp(value, "idle") == 0) {
        *state = APP_STATE_IDLE;
    } else if (strcmp(value, "running") == 0) {
        *state = APP_STATE_RUNNING;
    } else if (strcmp(value, "needs_input") == 0) {
        *state = APP_STATE_NEEDS_INPUT;
    } else if (strcmp(value, "ready") == 0) {
        *state = APP_STATE_READY;
    } else if (strcmp(value, "blocked") == 0) {
        *state = APP_STATE_BLOCKED;
    } else {
        return false;
    }
    return true;
}

static bool valid_reason(const char *value)
{
    return value != NULL &&
        (strcmp(value, "working") == 0 ||
            strcmp(value, "question") == 0 ||
            strcmp(value, "approval") == 0 ||
            strcmp(value, "permission") == 0 ||
            strcmp(value, "completed") == 0 ||
            strcmp(value, "failed") == 0);
}

static bool object_has_only_keys(
    const cJSON *object,
    const char *const *allowed,
    size_t allowed_count)
{
    if (!cJSON_IsObject(object)) {
        return false;
    }
    for (const cJSON *item = object->child; item != NULL; item = item->next) {
        bool known = false;
        for (size_t i = 0; i < allowed_count; ++i) {
            if (item->string != NULL && strcmp(item->string, allowed[i]) == 0) {
                known = true;
                break;
            }
        }
        if (!known) {
            return false;
        }
    }
    return true;
}

static bool copy_string(
    char *destination,
    size_t capacity,
    const cJSON *value,
    bool required)
{
    if (destination == NULL || capacity == 0) {
        return false;
    }
    destination[0] = '\0';
    if (!cJSON_IsString(value) || value->valuestring == NULL) {
        return !required;
    }
    size_t length = strlen(value->valuestring);
    if (length >= capacity) {
        return false;
    }
    memcpy(destination, value->valuestring, length + 1);
    return true;
}

static bool json_u64(const cJSON *value, uint64_t *result)
{
    if (!cJSON_IsNumber(value) || result == NULL ||
        !isfinite(value->valuedouble) || value->valuedouble < 0.0 ||
        floor(value->valuedouble) != value->valuedouble ||
        value->valuedouble > (double)UINT64_MAX) {
        return false;
    }
    *result = (uint64_t)value->valuedouble;
    return true;
}

static bool json_nonnegative_int(const cJSON *value, int *result)
{
    if (!cJSON_IsNumber(value) || result == NULL ||
        !isfinite(value->valuedouble) || value->valuedouble < 0.0 ||
        floor(value->valuedouble) != value->valuedouble ||
        value->valuedouble > 2147483647.0) {
        return false;
    }
    *result = value->valueint;
    return true;
}

static bool is_hex(char value)
{
    return (value >= '0' && value <= '9') ||
        (value >= 'a' && value <= 'f') ||
        (value >= 'A' && value <= 'F');
}

static bool is_canonical_uuid(const char *value)
{
    if (value == NULL || strlen(value) != 36 ||
        value[8] != '-' || value[13] != '-' ||
        value[18] != '-' || value[23] != '-') {
        return false;
    }
    for (size_t i = 0; i < 36; ++i) {
        if (i == 8 || i == 13 || i == 18 || i == 23) {
            continue;
        }
        if (!is_hex(value[i])) {
            return false;
        }
    }
    /* Match the canonical UUID shape accepted by the host router. */
    char version = (char)tolower((unsigned char)value[14]);
    char variant = (char)tolower((unsigned char)value[19]);
    return version >= '1' && version <= '8' &&
        (variant == '8' || variant == '9' || variant == 'a' || variant == 'b');
}

static bool parse_counts(const cJSON *object, app_snapshot_t *snapshot)
{
    static const char *const allowed[] = {
        "needsInput", "blocked", "ready", "running",
    };
    if (!cJSON_IsObject(object) || snapshot == NULL ||
        !object_has_only_keys(object, allowed, sizeof(allowed) / sizeof(allowed[0])) ||
        !json_nonnegative_int(
            cJSON_GetObjectItemCaseSensitive(object, "needsInput"),
            &snapshot->needs_input_count) ||
        !json_nonnegative_int(
            cJSON_GetObjectItemCaseSensitive(object, "blocked"),
            &snapshot->blocked_count) ||
        !json_nonnegative_int(
            cJSON_GetObjectItemCaseSensitive(object, "ready"),
            &snapshot->ready_count) ||
        !json_nonnegative_int(
            cJSON_GetObjectItemCaseSensitive(object, "running"),
            &snapshot->running_count)) {
        return false;
    }
    return true;
}

static bool parse_task(const cJSON *item, app_task_t *task)
{
    static const char *const allowed[] = {
        "taskPresenceId", "safeTitle", "state", "reason", "subagentCount",
        "capabilities", "updatedAt", "snoozed",
    };
    if (!cJSON_IsObject(item) || task == NULL) {
        return false;
    }
    if (!object_has_only_keys(item, allowed, sizeof(allowed) / sizeof(allowed[0]))) {
        return false;
    }
    memset(task, 0, sizeof(*task));
    task->feed_slot = -1;
    if (!copy_string(
            task->id,
            sizeof(task->id),
            cJSON_GetObjectItemCaseSensitive(item, "taskPresenceId"),
            true) ||
        !is_canonical_uuid(task->id) ||
        !copy_string(
            task->title,
            sizeof(task->title),
            cJSON_GetObjectItemCaseSensitive(item, "safeTitle"),
            true) ||
        !copy_string(
            task->reason,
            sizeof(task->reason),
            cJSON_GetObjectItemCaseSensitive(item, "reason"),
            true) ||
        !valid_reason(task->reason) ||
        !copy_string(
            task->updated_at,
            sizeof(task->updated_at),
            cJSON_GetObjectItemCaseSensitive(item, "updatedAt"),
            true) ||
        !json_nonnegative_int(
            cJSON_GetObjectItemCaseSensitive(item, "subagentCount"),
            &task->subagent_count) ||
        !cJSON_IsBool(cJSON_GetObjectItemCaseSensitive(item, "snoozed"))) {
        return false;
    }

    const cJSON *state = cJSON_GetObjectItemCaseSensitive(item, "state");
    if (!cJSON_IsString(state) || !parse_state(state->valuestring, &task->state)) {
        return false;
    }
    const cJSON *snoozed = cJSON_GetObjectItemCaseSensitive(item, "snoozed");
    task->snoozed = cJSON_IsTrue(snoozed);

    const cJSON *capabilities = cJSON_GetObjectItemCaseSensitive(item, "capabilities");
    if (!cJSON_IsArray(capabilities)) {
        return false;
    }
    const cJSON *capability = NULL;
    cJSON_ArrayForEach(capability, capabilities) {
        if (!cJSON_IsString(capability) || capability->valuestring == NULL ||
            strcmp(capability->valuestring, "focus") != 0) {
            /* Unknown capability names are not silently acted on. */
            return false;
        }
        task->focus_capability = true;
    }
    return true;
}

esp_err_t app_protocol_parse_snapshot(
    const char *json,
    size_t length,
    app_snapshot_t *snapshot)
{
    if (json == NULL || snapshot == NULL || length == 0) {
        return ESP_ERR_INVALID_ARG;
    }

    cJSON *root = cJSON_ParseWithLength(json, length);
    if (root == NULL || !cJSON_IsObject(root)) {
        cJSON_Delete(root);
        return ESP_ERR_INVALID_ARG;
    }
    const cJSON *schema = cJSON_GetObjectItemCaseSensitive(root, "schema");
    const cJSON *type = cJSON_GetObjectItemCaseSensitive(root, "type");
    if (!cJSON_IsString(schema) || strcmp(schema->valuestring, "agentagotchi.feed.v1") != 0 ||
        !cJSON_IsString(type) || strcmp(type->valuestring, "snapshot") != 0) {
        cJSON_Delete(root);
        return ESP_ERR_NOT_SUPPORTED;
    }

    /* Parse off to the side so an invalid replacement cannot partially erase
     * the last accepted contribution from this feed slot. */
    app_snapshot_t *candidate = calloc(1, sizeof(*candidate));
    if (candidate == NULL) {
        cJSON_Delete(root);
        return ESP_ERR_NO_MEM;
    }

    esp_err_t result = ESP_ERR_INVALID_ARG;
    const cJSON *origin = cJSON_GetObjectItemCaseSensitive(root, "origin");
    static const char *const allowed_root[] = {
        "schema", "type", "origin", "generatedAt", "aggregateState", "counts", "tasks",
    };
    static const char *const allowed_origin[] = {
        "kind", "id", "generation", "revision",
    };
    if (!object_has_only_keys(
            root,
            allowed_root,
            sizeof(allowed_root) / sizeof(allowed_root[0])) ||
        !object_has_only_keys(
            origin,
            allowed_origin,
            sizeof(allowed_origin) / sizeof(allowed_origin[0]))) {
        goto cleanup;
    }
    const cJSON *origin_kind = cJSON_GetObjectItemCaseSensitive(origin, "kind");
    const cJSON *origin_id = cJSON_GetObjectItemCaseSensitive(origin, "id");
    if (!cJSON_IsObject(origin) || !cJSON_IsString(origin_kind) ||
        (strcmp(origin_kind->valuestring, "edge") != 0 &&
            strcmp(origin_kind->valuestring, "home") != 0) ||
        !copy_string(
            candidate->origin_kind,
            sizeof(candidate->origin_kind),
            origin_kind,
            true) ||
        !copy_string(
            candidate->origin_id,
            sizeof(candidate->origin_id),
            origin_id,
            true) ||
        candidate->origin_id[0] == '\0' ||
        !json_u64(
            cJSON_GetObjectItemCaseSensitive(origin, "generation"),
            &candidate->origin_generation) ||
        !json_u64(
            cJSON_GetObjectItemCaseSensitive(origin, "revision"),
            &candidate->origin_revision) ||
        !cJSON_IsString(cJSON_GetObjectItemCaseSensitive(root, "generatedAt")) ||
        !cJSON_IsString(cJSON_GetObjectItemCaseSensitive(root, "aggregateState")) ||
        !parse_state(
            cJSON_GetObjectItemCaseSensitive(root, "aggregateState")->valuestring,
            &candidate->aggregate_state) ||
        !parse_counts(
            cJSON_GetObjectItemCaseSensitive(root, "counts"),
            candidate) ||
        !cJSON_IsArray(cJSON_GetObjectItemCaseSensitive(root, "tasks"))) {
        goto cleanup;
    }

    const cJSON *tasks = cJSON_GetObjectItemCaseSensitive(root, "tasks");
    const cJSON *item = NULL;
    cJSON_ArrayForEach(item, tasks) {
        if (candidate->task_count >= APP_MAX_TASKS) {
            result = ESP_ERR_INVALID_SIZE;
            goto cleanup;
        }
        if (!parse_task(item, &candidate->tasks[candidate->task_count])) {
            goto cleanup;
        }
        for (int previous = 0; previous < candidate->task_count; ++previous) {
            if (strcmp(
                    candidate->tasks[previous].id,
                    candidate->tasks[candidate->task_count].id) == 0) {
                goto cleanup;
            }
        }
        candidate->task_count++;
    }
    result = ESP_OK;

cleanup:
    if (result == ESP_OK) {
        *snapshot = *candidate;
    }
    free(candidate);
    cJSON_Delete(root);
    return result;
}

int app_protocol_state_priority(app_agent_state_t state)
{
    switch (state) {
    case APP_STATE_NEEDS_INPUT:
        return 4;
    case APP_STATE_BLOCKED:
        return 3;
    case APP_STATE_READY:
        return 2;
    case APP_STATE_RUNNING:
        return 1;
    case APP_STATE_IDLE:
    default:
        return 0;
    }
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
