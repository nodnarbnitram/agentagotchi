#include "app_font_symbols.h"

#include "font/fmt_txt/lv_font_fmt_txt.h"

/* U+2014 EM DASH, 8 pixels wide at the Montserrat 12 x-height. */
static LV_ATTRIBUTE_MEM_ALIGN const uint8_t glyph_bitmap[] = {
    0xff,
};

static const lv_font_fmt_txt_glyph_dsc_t glyph_dsc[] = {
    {
        .bitmap_index = 0,
        .adv_w = 0,
        .box_w = 0,
        .box_h = 0,
        .ofs_x = 0,
        .ofs_y = 0,
    },
    {
        .bitmap_index = 0,
        .adv_w = 144,
        .box_w = 8,
        .box_h = 1,
        .ofs_x = 0,
        .ofs_y = 4,
    },
};

static const lv_font_fmt_txt_cmap_t cmaps[] = {
    {
        .range_start = 0x2014,
        .range_length = 1,
        .glyph_id_start = 1,
        .unicode_list = NULL,
        .glyph_id_ofs_list = NULL,
        .list_length = 0,
        .type = LV_FONT_FMT_TXT_CMAP_FORMAT0_TINY,
    },
};

static const lv_font_fmt_txt_dsc_t font_dsc = {
    .glyph_bitmap = glyph_bitmap,
    .glyph_dsc = glyph_dsc,
    .cmaps = cmaps,
    .kern_dsc = NULL,
    .kern_scale = 0,
    .cmap_num = 1,
    .bpp = 1,
    .kern_classes = 0,
    .bitmap_format = LV_FONT_FMT_TXT_PLAIN,
    .stride = 0,
};

const lv_font_t app_font_em_dash_12 = {
    .get_glyph_dsc = lv_font_get_glyph_dsc_fmt_txt,
    .get_glyph_bitmap = lv_font_get_bitmap_fmt_txt,
    .release_glyph = NULL,
    .line_height = 15,
    .base_line = 3,
    .subpx = LV_FONT_SUBPX_NONE,
    .kerning = LV_FONT_KERNING_NORMAL,
    .static_bitmap = 0,
    .underline_position = -1,
    .underline_thickness = 1,
    .dsc = &font_dsc,
    .fallback = NULL,
    .user_data = NULL,
};
