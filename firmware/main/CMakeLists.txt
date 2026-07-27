idf_component_register(
    SRCS
        "app_main.c"
        "app_audio.c"
        "app_font_symbols.c"
        "app_network.c"
        "app_protocol.c"
        "app_sensors.c"
        "app_settings.c"
        "app_ui.c"
        "pet_asset.c"
        "sensor_math.c"
    INCLUDE_DIRS "."
    EMBED_FILES "assets/pet_device_rgb565.bin"
)
