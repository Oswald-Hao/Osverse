# Phase 1 manual acceptance

This checklist is the release gate for the Phase 1 read-only scanner. Automated tests are necessary but do not replace these GUI and VM checks. Every case below is **pending manual acceptance** until a tester records the date, image, artifact SHA-256, observations, and result.

## Test matrix and evidence

Use clean amd64 VMs for the supported matrix. Test the Ubuntu 20.04-built `build/bin/osverse` artifact on both releases.

| Environment | Shell | Status | Evidence |
| --- | --- | --- | --- |
| Ubuntu 20.04.x x86_64 | Bash | [ ] Pending manual | Tester/date/image/artifact hash: — |
| Ubuntu 20.04.x x86_64 | Zsh | [ ] Pending manual | Tester/date/image/artifact hash: — |
| Ubuntu 22.04.x x86_64 | Bash | [ ] Pending manual | Tester/date/image/artifact hash: — |
| Ubuntu 22.04.x x86_64 | Zsh | [ ] Pending manual | Tester/date/image/artifact hash: — |
| Ubuntu 22.04.x arm64 negative case | Bash | [ ] Pending manual | Tester/date/image/artifact hash: — |

Before each run, record these results and start the application from the same terminal so the tested PATH and SHELL are explicit:

```bash
sha256sum build/bin/osverse
file build/bin/osverse
ldd build/bin/osverse
printf 'PATH=%s\nSHELL=%s\n' "$PATH" "$SHELL"
cat /etc/os-release
uname -m
./build/bin/osverse
```

Restore a clean VM snapshot between scenarios. Test fixtures must never replace real tools on a developer workstation.

## Scanner cases

### A1 — No supported CLI installed

- [ ] Pending manual on Ubuntu 20.04 x86_64/Bash.
- [ ] Pending manual on Ubuntu 20.04 x86_64/Zsh.
- [ ] Pending manual on Ubuntu 22.04 x86_64/Bash.
- [ ] Pending manual on Ubuntu 22.04 x86_64/Zsh.

Use a fresh user with no `claude`, `codex`, or `opencode` executable in the process PATH or parsed profile PATHs. Confirm all three CLI cards show `missing`, contain no installation path, and the summary count agrees with the cards. Compare with `type -a claude codex opencode`; each lookup must fail.

### A2 — External CLI installation

- [ ] Pending manual on both supported Ubuntu releases and both shells.

Place one trusted test executable outside any Osverse-managed directory, add its absolute directory to PATH, and make `--version` print a catalog-valid version. Launch Osverse from that environment. Confirm exactly one installation is shown as installed, its displayed path and resolved path are correct, and it is not described as managed. Run the displayed absolute path with `--version` directly and confirm the dashboard version is identical.

### A3 — Conflicting CLI installations

- [ ] Pending manual on both supported Ubuntu releases.

Put two distinct, executable files with the same catalog command name in two absolute PATH directories. Both must return valid versions. Confirm the card reports `conflict`, lists both stable absolute paths in sorted order, and increments “needs attention.” Execute each displayed path directly with `--version` and compare both versions with the UI.

### A4 — Broken version command

- [ ] Pending manual on both supported Ubuntu releases.

Use one isolated fixture at a catalog command name that either exits non-zero, exceeds the timeout, or prints an invalid version. Confirm the card reports `broken`, does not invent a version, and the UI remains responsive. Record the direct absolute-path command, exit status, stdout, and stderr for comparison.

### A5 — Desktop evidence absent

- [ ] Pending manual on Ubuntu 20.04 x86_64.
- [ ] Pending manual on Ubuntu 22.04 x86_64.

On a clean VM, confirm the fixed package, executable, and desktop-file evidence is absent. Verify the dashboard reports `missing` when the component supports that OS and `unsupported` when its minimum Ubuntu version is higher. In particular, absent Claude Desktop is unsupported on 20.04 but missing on 22.04; absent ChatGPT Desktop is unsupported on both.

### A6 — Desktop evidence present

- [ ] Pending manual on Ubuntu 20.04 x86_64.
- [ ] Pending manual on Ubuntu 22.04 x86_64.

Install a test component normally, or use an isolated acceptance user with both a fixed-name desktop file in `~/.local/share/applications/` and its executable in PATH. Confirm the card reports installed only when installation evidence and an executable are both present. Remove the executable in the restored scenario and confirm the same evidence reports `broken`. Compare UI evidence with:

```bash
dpkg-query -W -f='${Package}\t${db:Status-Abbrev}\t${Version}\n' PACKAGE_NAME
test -f "$HOME/.local/share/applications/FIXED_NAME.desktop"
type -a EXECUTABLE_NAME
```

Do not execute the desktop file. Confirm Osverse does not alter its content or timestamps.

### A7 — Bash and Zsh PATH discovery

- [ ] Pending manual on Ubuntu 20.04 x86_64 with Bash and Zsh.
- [ ] Pending manual on Ubuntu 22.04 x86_64 with Bash and Zsh.

For each shell, put a fixture directory in the appropriate allowlisted profile using a simple absolute PATH assignment, then launch from that shell. Confirm the header reports the expected shell and the fixture is detected. Add a command substitution or another shell expression on a restored profile and confirm it is not executed or accepted as a path. Record the profile text and direct absolute-path version result.

### A8 — Unsupported architecture

- [ ] Pending manual on an Ubuntu 22.04 arm64 VM using a separately built arm64 negative-test artifact.

Confirm the system card reports architecture `arm64`, compatibility “unsupported,” and an x86_64-required reason. The UI must continue to render scan results without offering an enabled modification action. This negative check does not make arm64 a supported release target; do not try to run the amd64 artifact under transparent emulation for acceptance.

## GUI cases

### G1 — Responsive layout

- [ ] Pending manual on Ubuntu 20.04 x86_64.
- [ ] Pending manual on Ubuntu 22.04 x86_64.

Resize the Wails window through 320, 650, 900, 901, 960, 1024, 1053, 1060, and 1440 CSS pixels where the window manager permits. Confirm there is no horizontal page overflow, long paths wrap, cards do not overlap, and sidebar labels remain visible at desktop widths. Record screenshots at 320, 901, 1024, and 1440 pixels.

### G2 — Keyboard and accessibility

- [ ] Pending manual on Ubuntu 20.04 x86_64.
- [ ] Pending manual on Ubuntu 22.04 x86_64.

Navigate the full interface using the keyboard. Confirm focus is visible, focus order follows the visual order, refresh is named “刷新环境状态,” disabled future-action buttons cannot be activated, headings and landmarks are coherent, and scan/error notices are announced by a screen reader. Check text and status indicators at 200% zoom and with reduced motion enabled; meaning must not depend on color alone.

### G3 — Wails refresh lifecycle

- [ ] Pending manual on Ubuntu 20.04 x86_64.
- [ ] Pending manual on Ubuntu 22.04 x86_64.

Launch the production Wails binary, wait for the initial scan, then change one isolated fixture and select refresh. Confirm the previous snapshot remains visible while scanning, refresh is temporarily disabled, a single updated snapshot replaces the old one, and the timestamp advances. Remove a required fixture during a second refresh and confirm the new state matches direct commands without restarting the app.

### G4 — Direct-command parity

- [ ] Pending manual for every scanner scenario above.

For every path displayed by Osverse, invoke that exact absolute path with the catalog `--version` argument and record exit status/stdout/stderr. Compare package state with the exact `dpkg-query` fields shown in A6 and check only the fixed desktop-file paths. Any mismatch between direct evidence and the dashboard blocks promotion, even if the summary count looks correct.

## Sign-off

- [ ] All supported OS/shell rows are complete.
- [ ] The unsupported-architecture negative case is complete.
- [ ] Every mismatch has a linked issue or is resolved and rerun.
- [ ] Artifact SHA-256 and CI run URL are recorded.
- [ ] Tester confirms Phase 1 performed no install, update, configuration, or deletion action.

Release decision: **Pending manual acceptance**.
