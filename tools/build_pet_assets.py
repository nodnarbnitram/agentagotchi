#!/usr/bin/env python3
"""Build the documented Codex Pet v1 desktop and ESP32 assets."""

from __future__ import annotations

import argparse
import json
import struct
from pathlib import Path

from PIL import Image, ImageDraw

SHEET_WIDTH = 1536
SHEET_HEIGHT = 1872
COLUMNS = 8
ROWS = 13
CELL_WIDTH = SHEET_WIDTH // COLUMNS
CELL_HEIGHT = SHEET_HEIGHT // ROWS
DEVICE_WIDTH = CELL_WIDTH
DEVICE_HEIGHT = CELL_HEIGHT
DEVICE_STATES = ("idle", "running", "needs_input", "ready", "blocked")
ROW_NAMES = (
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
)
DEVICE_ROW = {
    "idle": 0,
    "running": 7,
    "needs_input": 12,
    "ready": 8,
    "blocked": 10,
}
BACKGROUND = (11, 21, 27, 255)
CYAN = (58, 231, 238, 255)
ORANGE = (255, 157, 31, 255)
RED = (255, 88, 88, 255)
GREEN = (95, 226, 154, 255)
CREAM = (255, 241, 188, 255)


def crop_subject(source: Image.Image) -> Image.Image:
    image = source.convert("RGBA")
    alpha = image.getchannel("A")
    bounds = alpha.getbbox()
    if bounds is None:
        raise ValueError("source image contains no visible pixels")
    return image.crop(bounds)


def resize_nearest(image: Image.Image, width: int, height: int) -> Image.Image:
    ratio = min(width / image.width, height / image.height)
    target = (
        max(1, round(image.width * ratio)),
        max(1, round(image.height * ratio)),
    )
    return image.resize(target, Image.Resampling.NEAREST)


def pixel_rect(draw: ImageDraw.ImageDraw, xy: tuple[int, int, int, int], fill: tuple[int, ...]) -> None:
    draw.rectangle(xy, fill=fill)


def add_state_mark(
    frame: Image.Image,
    row_name: str,
    phase: int,
) -> None:
    draw = ImageDraw.Draw(frame)
    pulse = 2 if phase % 2 else 0
    if row_name in {"waiting", "needs_input"}:
        x, y = 151, 26 - pulse
        pixel_rect(draw, (x, y, x + 10, y + 8), ORANGE)
        pixel_rect(draw, (x + 8, y + 8, x + 14, y + 18), ORANGE)
        pixel_rect(draw, (x + 6, y + 25, x + 12, y + 31), ORANGE)
    elif row_name in {"failed", "blocked"}:
        x, y = 149, 24
        color = RED
        for offset in range(0, 18, 5):
            pixel_rect(draw, (x + offset, y + offset, x + offset + 4, y + offset + 4), color)
            pixel_rect(draw, (x + 17 - offset, y + offset, x + 21 - offset, y + offset + 4), color)
    elif row_name in {"ready", "celebrate"}:
        x, y = 151, 25 - pulse
        pixel_rect(draw, (x + 8, y, x + 12, y + 26), GREEN)
        pixel_rect(draw, (x - 3, y + 10, x + 23, y + 14), GREEN)
        pixel_rect(draw, (x + 1, y + 4, x + 19, y + 20), GREEN)
        pixel_rect(draw, (x + 5, y + 8, x + 15, y + 16), CREAM)
    elif row_name == "running":
        x, y = 149, 27
        for i in range(3):
            height = 8 + ((phase + i) % 3) * 5
            pixel_rect(draw, (x + i * 8, y + 22 - height, x + i * 8 + 4, y + 22), CYAN)
    elif row_name == "sleep":
        x, y = 146, 26
        for i in range(3):
            size = 6 + i * 2
            pixel_rect(draw, (x + i * 10, y - i * 7, x + i * 10 + size, y - i * 7 + 4), CYAN)


def make_frame(subject: Image.Image, row_name: str, phase: int) -> Image.Image:
    frame = Image.new("RGBA", (CELL_WIDTH, CELL_HEIGHT), (0, 0, 0, 0))
    bob = (0, -2, -4, -2, 0, 1, 0, -1)[phase]
    x_shift = 0
    if row_name == "run_right":
        x_shift = (-7, -4, 0, 4, 7, 4, 0, -4)[phase]
    elif row_name == "run_left":
        x_shift = (7, 4, 0, -4, -7, -4, 0, 4)[phase]
    elif row_name == "jump":
        bob += (0, -5, -10, -15, -18, -15, -10, -5)[phase]
    elif row_name in {"failed", "blocked"}:
        bob += (2, 3, 2, 4, 2, 3, 2, 3)[phase]
    elif row_name in {"ready", "celebrate"}:
        bob += (0, -4, -1, -5, 0, -4, -1, -5)[phase]
    elif row_name == "sleep":
        bob += 4

    width = 104
    height = 116
    if row_name in {"running", "run_left", "run_right"} and phase % 2:
        width += 4
        height -= 3
    sprite = resize_nearest(subject, width, height)
    if row_name == "run_left":
        sprite = sprite.transpose(Image.Transpose.FLIP_LEFT_RIGHT)
    x = (CELL_WIDTH - sprite.width) // 2 + x_shift
    y = (CELL_HEIGHT - sprite.height) // 2 + bob
    frame.alpha_composite(sprite, (x, y))
    add_state_mark(frame, row_name, phase)
    return frame


def build_sheet(subject: Image.Image) -> Image.Image:
    sheet = Image.new("RGBA", (SHEET_WIDTH, SHEET_HEIGHT), (0, 0, 0, 0))
    for row, row_name in enumerate(ROW_NAMES):
        for column in range(COLUMNS):
            frame = make_frame(subject, row_name, column)
            sheet.alpha_composite(frame, (column * CELL_WIDTH, row * CELL_HEIGHT))
    return sheet


def rgb565_bytes(image: Image.Image) -> bytes:
    rgb = image.convert("RGB")
    raw = rgb.tobytes()
    output = bytearray()
    for index in range(0, len(raw), 3):
        red, green, blue = raw[index : index + 3]
        value = ((red & 0xF8) << 8) | ((green & 0xFC) << 3) | (blue >> 3)
        output.extend(struct.pack("<H", value))
    return bytes(output)


def build_device_binary(sheet: Image.Image) -> bytes:
    frames = bytearray()
    for state in DEVICE_STATES:
        row = DEVICE_ROW[state]
        for column in range(COLUMNS):
            box = (
                column * CELL_WIDTH,
                row * CELL_HEIGHT,
                (column + 1) * CELL_WIDTH,
                (row + 1) * CELL_HEIGHT,
            )
            source = sheet.crop(box)
            source = source.resize((DEVICE_WIDTH, DEVICE_HEIGHT), Image.Resampling.NEAREST)
            device_frame = Image.new("RGBA", source.size, BACKGROUND)
            device_frame.alpha_composite(source)
            frames.extend(rgb565_bytes(device_frame))
    header = struct.pack(
        "<4sHHHBBI",
        b"CPET",
        1,
        DEVICE_WIDTH,
        DEVICE_HEIGHT,
        len(DEVICE_STATES),
        COLUMNS,
        16,
    )
    return header + frames


def make_square_logo(subject: Image.Image, size: int, background: tuple[int, ...] | None) -> Image.Image:
    image = Image.new("RGBA", (size, size), background or (0, 0, 0, 0))
    sprite = resize_nearest(subject, round(size * 0.76), round(size * 0.76))
    image.alpha_composite(sprite, ((size - sprite.width) // 2, (size - sprite.height) // 2))
    return image


def write_outputs(root: Path, source_path: Path) -> None:
    subject = crop_subject(Image.open(source_path))
    sheet = build_sheet(subject)
    generated = root / "assets" / "generated"
    firmware_assets = root / "firmware" / "main" / "assets"
    plugin_assets = root / "plugin" / "codex-pet-status" / "assets"
    for directory in (generated, firmware_assets, plugin_assets):
        directory.mkdir(parents=True, exist_ok=True)

    sheet_path = generated / "codex-pet-v1.png"
    sheet.save(sheet_path, optimize=True)
    preview = Image.new("RGBA", sheet.size, BACKGROUND)
    preview.alpha_composite(sheet)
    preview.resize((768, 936), Image.Resampling.NEAREST).save(
        generated / "codex-pet-v1-preview.png", optimize=True
    )
    (firmware_assets / "pet_device_rgb565.bin").write_bytes(build_device_binary(sheet))

    make_square_logo(subject, 128, None).save(plugin_assets / "icon.png", optimize=True)
    make_square_logo(subject, 512, (244, 250, 249, 255)).save(
        plugin_assets / "logo.png", optimize=True
    )
    make_square_logo(subject, 512, BACKGROUND).save(
        plugin_assets / "logo-dark.png", optimize=True
    )

    metadata = {
        "name": "Codex Pet v1",
        "version": 1,
        "sheet": {
            "width": SHEET_WIDTH,
            "height": SHEET_HEIGHT,
            "columns": COLUMNS,
            "rows": ROWS,
            "cellWidth": CELL_WIDTH,
            "cellHeight": CELL_HEIGHT,
            "frameDurationMs": 100,
            "rowNames": list(ROW_NAMES),
            "pixelArt": True,
            "transparent": True,
        },
        "device": {
            "format": "CPET RGB565 little-endian",
            "width": DEVICE_WIDTH,
            "height": DEVICE_HEIGHT,
            "backgroundRgb": list(BACKGROUND[:3]),
            "states": list(DEVICE_STATES),
            "framesPerState": COLUMNS,
            "stateOrderMatchesFirmwareEnum": True,
        },
    }
    (generated / "codex-pet-v1.json").write_text(
        json.dumps(metadata, indent=2) + "\n", encoding="utf-8"
    )


def main() -> None:
    parser = argparse.ArgumentParser()
    parser.add_argument("--root", type=Path, default=Path(__file__).resolve().parents[1])
    parser.add_argument(
        "--source",
        type=Path,
        default=None,
        help="transparent pet master; defaults to assets/source/pet_base.png",
    )
    arguments = parser.parse_args()
    root = arguments.root.resolve()
    source = arguments.source or root / "assets" / "source" / "pet_base.png"
    write_outputs(root, source)


if __name__ == "__main__":
    main()
