#!/usr/bin/env python3
"""Build platform icon files from the single Ramseyer app-icon master."""

from pathlib import Path

try:
    from PIL import Image, ImageDraw
except ImportError as error:
    # Icon generation is a release-maintenance task, not an application runtime
    # dependency. A focused message here prevents a maintainer from adding Pillow to the
    # Go application's deployment just because their packaging workstation lacks it.
    raise SystemExit(
        "Pillow is required only to rebuild icons: python3 -m pip install Pillow"
    ) from error


ROOT_DIR = Path(__file__).resolve().parent.parent
MASTER_ICON = ROOT_DIR / "static" / "app-icon.png"
MASTER_SIZE = 1024
ICON_BODY_SIZE = 824
ICON_CORNER_RADIUS = 185


def prepare_master(source: Image.Image) -> Image.Image:
    """Return a native-looking macOS icon with transparent exterior corners."""

    icon = source.convert("RGBA")
    corners = (
        icon.getpixel((0, 0))[3],
        icon.getpixel((icon.width - 1, 0))[3],
        icon.getpixel((0, icon.height - 1))[3],
        icon.getpixel((icon.width - 1, icon.height - 1))[3],
    )
    if icon.size == (MASTER_SIZE, MASTER_SIZE) and corners == (0, 0, 0, 0):
        return icon

    # A macOS application icon is artwork inside a rounded-square body, not a square
    # photograph whose corners happen to be hidden by CSS. The transparent 100px safe
    # area keeps the body at the same visual scale as native Dock icons and lets macOS
    # render the silhouette cleanly in the Dock, Finder, Spotlight, and the app switcher.
    crop_size = min(icon.size) * 0.86
    left = (icon.width - crop_size) / 2
    top = (icon.height - crop_size) / 2
    artwork = icon.crop((left, top, left + crop_size, top + crop_size)).resize(
        (ICON_BODY_SIZE, ICON_BODY_SIZE), Image.Resampling.LANCZOS
    )

    # Drawing the mask at 4x resolution and reducing it produces a smooth antialiased
    # edge without the pale halo that appears when opaque source corners are merely
    # recoloured. The pixels outside this mask are genuine alpha transparency.
    mask_scale = 4
    mask = Image.new(
        "L", (ICON_BODY_SIZE * mask_scale, ICON_BODY_SIZE * mask_scale), 0
    )
    ImageDraw.Draw(mask).rounded_rectangle(
        (0, 0, mask.width - 1, mask.height - 1),
        radius=ICON_CORNER_RADIUS * mask_scale,
        fill=255,
    )
    mask = mask.resize((ICON_BODY_SIZE, ICON_BODY_SIZE), Image.Resampling.LANCZOS)
    artwork.putalpha(mask)

    canvas = Image.new("RGBA", (MASTER_SIZE, MASTER_SIZE), (0, 0, 0, 0))
    offset = (MASTER_SIZE - ICON_BODY_SIZE) // 2
    canvas.alpha_composite(artwork, (offset, offset))
    return canvas


def main() -> None:
    if not MASTER_ICON.is_file():
        raise SystemExit(f"Missing master icon: {MASTER_ICON}")

    with Image.open(MASTER_ICON) as source:
        icon = prepare_master(source)

        # Keep the checked-in master identical to the input used for every platform. This
        # also makes the conversion idempotent: once the transparent corners exist, later
        # rebuilds validate and reuse them instead of repeatedly shrinking the artwork.
        icon.save(MASTER_ICON, format="PNG")

        # Pillow writes the complete multi-resolution ICNS family, including Retina sizes.
        # A single 1024px-only image can appear blurred in Finder's small list views, so the
        # derived file deliberately carries the platform's size variants.
        icon.save(ROOT_DIR / "static" / "app-icon.icns", format="ICNS")

        # Explorer and the Windows taskbar request different embedded dimensions. Supplying
        # the full set avoids letting Windows downscale a single large bitmap at runtime.
        icon.save(
            ROOT_DIR / "static" / "app-icon.ico",
            format="ICO",
            sizes=[
                (16, 16),
                (24, 24),
                (32, 32),
                (48, 48),
                (64, 64),
                (128, 128),
                (256, 256),
            ],
        )

        # The browser shell only needs a compact favicon. LANCZOS preserves the thin crest
        # lettering better than a nearest-neighbour resize at this small display size.
        favicon = icon.resize((64, 64), Image.Resampling.LANCZOS)
        favicon.save(ROOT_DIR / "static" / "app-icon-64.png", format="PNG")

    print("Built static/app-icon.icns, static/app-icon.ico, and static/app-icon-64.png")


if __name__ == "__main__":
    main()
