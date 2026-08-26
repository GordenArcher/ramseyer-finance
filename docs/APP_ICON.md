# App Icon

The application icon is based on the official **P.C.G. Ramseyer Congregation, Afiaman** crest published at [ramseyerafiaman.org](https://ramseyerafiaman.org/). This provenance note is important because image searches also return unrelated Ramseyer congregations in Kumasi and Abetifi.

## Source and derived files

- `static/app-icon.png` is the project master adapted from the official Afiaman crest.
- `static/app-icon.icns` is embedded in the macOS application bundle.
- `static/app-icon.ico` is the multi-resolution Windows source.
- `static/app-icon-64.png` is displayed by the webview, browser tab, login screen, and sidebar.
- `rsrc_windows_amd64.syso` and the matching resources under `cmd/launcher/` and `cmd/updater/` embed the icon into all Windows executables.

Run `./scripts/build_app_icons.py` after changing the master. The script requires Pillow on the packaging workstation and regenerates every derived image from the same source, preventing platform branding from drifting.

## Generation brief

The master was produced with the built-in image-generation workflow using the official crest as an identity-preserving reference. The edit kept the complete shield, stars, ring, wording, and PCG colours, replaced the old spotlight background with the application's deep forest green, and added restrained gold edge lighting suitable for a finance application icon.
