#!/usr/bin/env python3
"""Build platform icon files from the single Ramseyer app-icon master."""

from pathlib import Path

try:
    from PIL import Image
except ImportError as error:
    # Icon generation is a release-maintenance task, not an application runtime
    # dependency. A focused message here prevents a maintainer from adding Pillow to the
    # Go application's deployment just because their packaging workstation lacks it.
    raise SystemExit(
        "Pillow is required only to rebuild icons: python3 -m pip install Pillow"
    ) from error


ROOT_DIR = Path(__file__).resolve().parent.parent
MASTER_ICON = ROOT_DIR / "static" / "app-icon.png"


def main() -> None:
    if not MASTER_ICON.is_file():
        raise SystemExit(f"Missing master icon: {MASTER_ICON}")

    # I normalize to RGBA before writing platform formats because generated source images
    # may be RGB today and transparent tomorrow. Keeping one conversion path prevents an
    # innocuous source-mode change from producing a generic icon in only one package.
    with Image.open(MASTER_ICON) as source:
        icon = source.convert("RGBA")

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
