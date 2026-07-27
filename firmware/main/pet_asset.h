#pragma once

#include <stdbool.h>
#include <stddef.h>
#include <stdint.h>

#include "app_state.h"

typedef struct {
    uint16_t width;
    uint16_t height;
    uint8_t states;
    uint8_t frames;
    const uint8_t *pixels;
    size_t pixel_bytes;
} pet_asset_t;

bool pet_asset_open(pet_asset_t *asset);
bool pet_asset_copy_frame(
    const pet_asset_t *asset,
    app_agent_state_t state,
    uint8_t frame,
    uint16_t *destination,
    size_t destination_pixels
);
