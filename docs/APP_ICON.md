# App Icon

The application icon is based on the official **P.C.G. Ramseyer Congregation, Afiaman** crest published at [ramseyerafiaman.org](https://ramseyerafiaman.org/). This provenance note is important because image searches also return unrelated Ramseyer congregations in Kumasi and Abetifi.

## Source and derived files

- `static/app-icon.png` is the 1024px project master adapted from the official Afiaman crest. Its artwork sits inside a macOS-style rounded square with genuine transparent corners and native-safe spacing.
- `static/app-icon.icns` is embedded in the macOS application bundle.
- `static/app-icon.ico` is the multi-resolution Windows source.
- `static/app-icon-64.png` is displayed by the webview and browser tab; the full-size master remains visible on the login screen.
- `rsrc_windows_amd64.syso` and the matching resources under `cmd/launcher/` and `cmd/updater/` embed the icon into all Windows executables.

Run `./scripts/build_app_icons.py` after changing the master. The script requires Pillow on the packaging workstation, converts opaque source artwork to the rounded native silhouette when needed, and regenerates every derived image from the same source, preventing platform branding from drifting.

## Generation brief

The master was produced with the built-in image-generation workflow using the official crest as an identity-preserving reference. The edit kept the complete shield, stars, ring, wording, and PCG colours, replaced the old spotlight background with the application's deep forest green, and added restrained gold edge lighting suitable for a finance application icon.
