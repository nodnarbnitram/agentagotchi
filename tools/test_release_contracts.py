#!/usr/bin/env python3

import plistlib
import re
import unittest
from pathlib import Path

import yaml


ROOT = Path(__file__).resolve().parents[1]


class ReleaseContractTests(unittest.TestCase):
    def test_firmware_dependencies_are_pinned_and_locked(self) -> None:
        manifest = yaml.safe_load(
            (ROOT / "firmware/main/idf_component.yml").read_text(encoding="utf-8")
        )["dependencies"]
        lock = yaml.safe_load(
            (ROOT / "firmware/dependencies.lock").read_text(encoding="utf-8")
        )["dependencies"]

        self.assertEqual(manifest["idf"], ">=5.5.0,<5.6.0")
        expected = {
            "espressif/esp-box-3": "3.2.0",
            "espressif/esp_websocket_client": "1.7.0",
            "espressif/aht30": "1.0.0~1",
            "espressif/mdns": "1.11.3",
        }
        for component, version in expected.items():
            self.assertEqual(manifest[component], version)
            self.assertEqual(lock[component]["version"], version)
            self.assertTrue(lock[component]["component_hash"])
        self.assertEqual(lock["idf"]["version"], "5.5.5")

    def test_sensor_build_contract(self) -> None:
        kconfig = (ROOT / "firmware/main/Kconfig.projbuild").read_text(
            encoding="utf-8"
        )
        defaults = (ROOT / "firmware/sdkconfig.defaults").read_text(encoding="utf-8")
        main = (ROOT / "firmware/main/app_main.c").read_text(encoding="utf-8")
        state = (ROOT / "firmware/main/app_state.h").read_text(encoding="utf-8")
        sensors = (ROOT / "firmware/main/app_sensors.c").read_text(encoding="utf-8")
        audio = (ROOT / "firmware/main/app_audio.c").read_text(encoding="utf-8")
        network = (ROOT / "firmware/main/app_network.c").read_text(
            encoding="utf-8"
        )
        ui = (ROOT / "firmware/main/app_ui.c").read_text(encoding="utf-8")
        symbols = (ROOT / "firmware/main/app_font_symbols.c").read_text(
            encoding="utf-8"
        )

        self.assertRegex(
            kconfig,
            r"config CODEX_PET_SENSOR_BAR\s+bool .*?\s+default y",
        )
        self.assertIn("CONFIG_CODEX_PET_SENSOR_BAR=y", defaults)
        self.assertIn("CONFIG_ESP_MAIN_TASK_STACK_SIZE=8192", defaults)
        self.assertIn("CONFIG_MBEDTLS_EXTERNAL_MEM_ALLOC=y", defaults)
        self.assertIn("static app_ui_event_t event", main)
        self.assertIn("#define APP_TASK_TITLE_MAX 97", state)
        self.assertIn("xQueueCreateStatic(", main)
        self.assertIn("MALLOC_CAP_SPIRAM | MALLOC_CAP_8BIT", main)
        self.assertNotIn("app_ui_event_t discarded;", main)
        self.assertIn("static app_ui_event_t event", ui)
        expected_defines = {
            "RADAR_GPIO": "GPIO_NUM_21",
            "BATTERY_CHANNEL": "ADC_CHANNEL_9",
            "BATTERY_SAMPLES": "64",
        }
        for name, value in expected_defines.items():
            self.assertRegex(sensors, rf"#define {name}\s+{value}\b")
        self.assertIn("#define ENV_INTERVAL_US (30LL * 1000000LL)", sensors)
        self.assertIn("#define BATTERY_INTERVAL_US (60LL * 1000000LL)", sensors)
        self.assertIn("#define SENSOR_STALE_US (5LL * 60LL * 1000000LL)", sensors)
        self.assertIn("#define PRESENCE_HOLD_US (30LL * 1000000LL)", sensors)
        self.assertIn("bsp_i2c_init()", sensors)
        self.assertRegex(
            sensors,
            r"#define SENSOR_I2C_PORT "
            r"\(BSP_I2C_NUM == I2C_NUM_1 \? I2C_NUM_0 : I2C_NUM_1\)",
        )
        self.assertIn("#define STATUS_BAR_HEIGHT 20", ui)
        self.assertIn("#define STATUS_REDRAW_MS 1000", ui)
        self.assertIn("#define PET_WIDTH 192", ui)
        self.assertIn("#define PET_HEIGHT 144", ui)
        self.assertIn("#define BUBBLE_MAX_WIDTH 304", ui)
        self.assertIn("#define BUBBLE_TITLE_MAX_LINES 3", ui)
        self.assertIn("speech_bubble", ui)
        self.assertIn("layout_speech_bubble();", ui)
        self.assertIn("lv_text_get_size(", ui)
        self.assertIn("LV_LABEL_LONG_MODE_DOTS", ui)
        self.assertIn('"—"', ui)
        self.assertIn(".fallback = &app_font_em_dash_12", ui)
        self.assertIn(".range_start = 0x2014", symbols)
        self.assertIn("i2c_master_probe(", audio)
        self.assertIn("#define CODEC_STABLE_PROBES 3", audio)
        self.assertLess(
            audio.index("if (codec_is_ready())"),
            audio.index("bsp_audio_codec_speaker_init()"),
        )
        self.assertIn("discovered_ipv4(", network)
        self.assertIn("IP2STR(&address->addr.u_addr.ip4)", network)
        self.assertIn(
            ".cert_common_name = context->settings.bridge_host",
            network,
        )
        self.assertIn("#define WS_RX_MAX 8192", network)
        for source in (audio, sensors, network):
            self.assertIn("xTaskCreatePinnedToCoreWithCaps(", source)
            self.assertIn("MALLOC_CAP_SPIRAM | MALLOC_CAP_8BIT", source)

    def test_launch_agent_contract(self) -> None:
        template = (ROOT / "packaging/com.openai.codexpet.plist.in").read_text(
            encoding="utf-8"
        )
        parsed = plistlib.loads(template.encode("utf-8"))
        self.assertEqual(parsed["Label"], "com.openai.codexpet")
        self.assertTrue(parsed["RunAtLoad"])
        self.assertTrue(parsed["KeepAlive"])
        self.assertEqual(parsed["ProgramArguments"][1], "serve")


if __name__ == "__main__":
    unittest.main()
