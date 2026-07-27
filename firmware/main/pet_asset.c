#include "pet_asset.h"

#include <string.h>

extern const uint8_t pet_binary_start[] asm("_binary_pet_device_rgb565_bin_start");
extern const uint8_t pet_binary_end[] asm("_binary_pet_device_rgb565_bin_end");

#define PET_HEADER_SIZE 16
#define PET_ASSET_VERSION 1

static uint16_t read_u16(const uint8_t *p)
{
    return (uint16_t)p[0] | ((uint16_t)p[1] << 8);
}

static uint32_t read_u32(const uint8_t *p)
{
    return (uint32_t)p[0] |
        ((uint32_t)p[1] << 8) |
        ((uint32_t)p[2] << 16) |
        ((uint32_t)p[3] << 24);
}

bool pet_asset_open(pet_asset_t *asset)
{
    if (asset == NULL) {
        return false;
    }
    size_t total = (size_t)(pet_binary_end - pet_binary_start);
    if (total < PET_HEADER_SIZE || memcmp(pet_binary_start, "CPET", 4) != 0 ||
        read_u16(pet_binary_start + 4) != PET_ASSET_VERSION) {
        return false;
    }
    uint16_t width = read_u16(pet_binary_start + 6);
    uint16_t height = read_u16(pet_binary_start + 8);
    uint8_t states = pet_binary_start[10];
    uint8_t frames = pet_binary_start[11];
    uint32_t offset = read_u32(pet_binary_start + 12);
    size_t expected = (size_t)width * height * states * frames * sizeof(uint16_t);
    if (width == 0 || height == 0 || states < 5 || frames == 0 ||
        offset < PET_HEADER_SIZE || offset > total || expected > total - offset) {
        return false;
    }
    asset->width = width;
    asset->height = height;
    asset->states = states;
    asset->frames = frames;
    asset->pixels = pet_binary_start + offset;
    asset->pixel_bytes = expected;
    return true;
}

bool pet_asset_copy_frame(
    const pet_asset_t *asset,
    app_agent_state_t state,
    uint8_t frame,
    uint16_t *destination,
    size_t destination_pixels)
{
    if (asset == NULL || destination == NULL ||
        destination_pixels < (size_t)asset->width * asset->height) {
        return false;
    }
    uint8_t state_index = (uint8_t)state;
    if (state_index >= asset->states) {
        state_index = 0;
    }
    frame %= asset->frames;
    size_t frame_pixels = (size_t)asset->width * asset->height;
    size_t frame_index = ((size_t)state_index * asset->frames) + frame;
    memcpy(destination, asset->pixels + frame_index * frame_pixels * sizeof(uint16_t),
        frame_pixels * sizeof(uint16_t));
    return true;
}
