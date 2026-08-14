# Linux release packaging

`package.sh` turns one already-built, executable Wails binary into a reproducible
tar archive and Debian package. It accepts only an explicit package version and
the two supported release target names, `ubuntu20.04` and `ubuntu22.04`.

The `.deb` installs `/usr/bin/osverse`, its desktop entry, icon, documentation,
and declares the GTK/WebKitGTK, PolicyKit, GnuPG, and CA-certificate runtime
dependencies. It never runs maintainer scripts.
