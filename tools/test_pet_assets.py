#!/usr/bin/env python3

import json
import re
import struct
import unittest
from pathlib import Path

from PIL import Image


ROOT = Path(__file__).resolve().parents[1]


class PetAssetTests(unittest.TestCase):
    def test_documented_sheet_shape_and_alpha(self) -> None:
        sheet = Image.open(ROOT / "assets/generated/agentagotchi-v1.png")
        self.assertEqual(sheet.size, (1536, 1872))
        self.assertEqual(sheet.mode, "RGBA")
        alpha = sheet.getchannel("A")
        self.assertEqual(alpha.getextrema(), (0, 255))
        populated_alpha_values = {
            value for value, count in enumerate(alpha.histogram()) if count
        }
        self.assertEqual(populated_alpha_values, {0, 255})

        for row in range(13):
            for column in range(8):
                cell_alpha = sheet.crop(
                    (
                        column * 192,
                        row * 144,
                        (column + 1) * 192,
                        (row + 1) * 144,
                    )
                ).getchannel("A")
                self.assertEqual(
                    cell_alpha.getextrema(),
                    (0, 255),
                    f"sprite cell row={row} column={column} is empty or opaque",
                )

    def test_rgb565_header_and_length(self) -> None:
        data = (ROOT / "firmware/main/assets/pet_device_rgb565.bin").read_bytes()
        magic, version, width, height, states, frames, offset = struct.unpack(
            "<4sHHHBBI", data[:16]
        )
        self.assertEqual(magic, b"AGOT")
        self.assertEqual(version, 1)
        self.assertEqual((width, height, states, frames, offset), (192, 144, 5, 8, 16))
        self.assertEqual(len(data), 16 + width * height * states * frames * 2)
        background_rgb565 = (11 & 0xF8) << 8 | (21 & 0xFC) << 3 | 27 >> 3
        first_pixel = struct.unpack("<H", data[offset : offset + 2])[0]
        self.assertEqual(first_pixel, background_rgb565)

    def test_metadata_matches_sheet(self) -> None:
        metadata = json.loads(
            (ROOT / "assets/generated/agentagotchi-v1.json").read_text(encoding="utf-8")
        )
        self.assertEqual(
            metadata["sheet"],
            {
                "width": 1536,
                "height": 1872,
                "columns": 8,
                "rows": 13,
                "cellWidth": 192,
                "cellHeight": 144,
                "frameDurationMs": 100,
                "rowNames": [
                    "idle",
                    "run_right",
                    "run_left",
                    "wave",
                    "jump",
                    "failed",
                    "waiting",
                    "running",
                    "ready",
                    "celebrate",
                    "blocked",
                    "sleep",
                    "needs_input",
                ],
                "pixelArt": True,
                "transparent": True,
            },
        )
        self.assertEqual(
            metadata["device"]["states"],
            ["idle", "running", "needs_input", "ready", "blocked"],
        )
        self.assertEqual(metadata["device"]["backgroundRgb"], [11, 21, 27])
        self.assertEqual(metadata["device"]["framesPerState"], 8)
        self.assertTrue(metadata["device"]["stateOrderMatchesFirmwareEnum"])

    def test_device_state_order_matches_firmware_enum(self) -> None:
        header = (ROOT / "firmware/main/app_state.h").read_text(encoding="utf-8")
        enum_match = re.search(
            r"typedef enum \{(?P<body>.*?)\} app_agent_state_t;",
            header,
            flags=re.DOTALL,
        )
        self.assertIsNotNone(enum_match)
        names = re.findall(r"\bAPP_STATE_([A-Z_]+)\b", enum_match.group("body"))
        self.assertEqual(
            names,
            ["IDLE", "RUNNING", "NEEDS_INPUT", "READY", "BLOCKED"],
        )

    def test_plugin_art_is_generated_at_documented_sizes(self) -> None:
        expected = {
            "icon.png": ((128, 128), (0, 255)),
            "logo.png": ((512, 512), (255, 255)),
            "logo-dark.png": ((512, 512), (255, 255)),
        }
        for name, (size, alpha_extrema) in expected.items():
            image = Image.open(ROOT / "plugin/agentagotchi-status/assets" / name)
            self.assertEqual(image.size, size)
            self.assertEqual(image.mode, "RGBA")
            self.assertEqual(image.getchannel("A").getextrema(), alpha_extrema)


if __name__ == "__main__":
    unittest.main()
