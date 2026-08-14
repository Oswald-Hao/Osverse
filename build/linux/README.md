# Linux release packaging

`fetch-appimage-tools.sh` downloads the pinned AppImage tooling and verifies the
published SHA-256 values. `package.sh` turns one already-built, executable Wails
binary into a reproducible tar archive, Debian package, and AppImage. It accepts
only an explicit package version and the two supported release target names,
`ubuntu20.04` and `ubuntu22.04`.

The `.deb` installs `/usr/bin/osverse`, its desktop entry, icon, documentation,
and declares the GTK/WebKitGTK, PolicyKit, GnuPG, and CA-certificate runtime
dependencies. It never runs maintainer scripts.

The AppImage contains the Osverse binary, AppRun launcher, desktop metadata,
icon, and AppStream metadata. GTK and WebKitGTK remain platform libraries so the
Ubuntu 20.04 and 22.04 builds retain their tested ABI baselines.
