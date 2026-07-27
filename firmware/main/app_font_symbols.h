#pragma once

#include "lvgl.h"

/*
 * Montserrat 12 contains the degree sign used by the status bar, but not the
 * Unicode em dash used for unavailable sensor values. This one-glyph font is
 * attached as its fallback so the rest of the UI can keep using the compact
 * built-in font.
 */
LV_FONT_DECLARE(app_font_em_dash_12);
