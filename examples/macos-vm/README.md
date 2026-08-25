# Running `mav` against a macOS app inside a disposable VM

This is Phase 4 of the [scope evaluation](../../docs/macos-scope-evaluation.md), and what
matters is what is **not** here: MAV code. MAV does not orchestrate machines. It wraps
drivers and produces evidence; the machine comes from [crabbox](https://github.com/openclaw/crabbox),
which already knows how to lease a macOS VM with `tart`, sync the dirty checkout, execute,
and hand it back when done.

## Why bother

On your own Mac, validating a macOS app has two problems that code does not fix:

1. **TCC.** A real app asks for several permissions (microphone, calendar, Apple Events...).
   Either you already granted them, and then your test does not start clean, or you eat a
   prompt mid-run. And Screen Recording and microphone are *deny-only* in PPPC: not even
   an MDM admin can pre-authorize them.
2. **State.** `--clear-state` wipes the app container, but not the rest of the traces it
   leaves on the system.

In a VM whose image you build yourself, the second one disappears: throw the VM away and the state goes with it.

The first one does **not**, and this contradicts what most guides on the subject say.

### Seeding `TCC.db` does NOT work on macOS 26

Tested on a real macOS 26.0 VM with SIP disabled, and it fails in all four variants:

| Attempt | Result |
|---|---|
| `INSERT` by path, `client_type=1` | ignored |
| Signing identifier, `client_type=0`, no `csreq` | ignored |
| Signing identifier, `client_type=0`, **with a valid 172-byte `csreq`** | ignored |
| The above over the whole responsible process chain (`bash`, `zsh`, `ssh`, `sshd`) | ignored |

With `tccd` restarted on every attempt, and a full VM reboot at the end. `csrutil status`
reported `disabled` the whole time, and the rows stayed in the database with `auth_value=2`.

The guides that say this works, and the CircleCI orb, are from earlier macOS versions. On
macOS 26, writing to the system `TCC.db` stops being enough even with SIP disabled.

### What does work: granting through the hypervisor's virtual mouse

The permission cannot be granted **from inside** the VM: any way of pressing the button in
System Settings would already need the accessibility permission you are trying to grant. It
is circular.

But it can **from outside**. The VM has a virtual keyboard and mouse at the hypervisor level
(`configuration.keyboards` and `configuration.pointingDevices` in tart's `VM.swift`), and tart
exposes a VNC server against them (`tart run --vnc-experimental`, which also works before
login and in recovery mode). To the guest, those events are **hardware**, not synthetic
events from a process, and TCC only governs the synthetic ones.

Verified on a macOS 26.0 VM with zero permissions granted:

```sh
tart run mav-macos-test --no-graphics --vnc-experimental
# prints: VNC server is running at vnc://:<password>@127.0.0.1:<port>

vncdo -s 127.0.0.1::<port> -p <password> capture screen.png   # → full framebuffer
vncdo -s 127.0.0.1::<port> -p <password> move 52 28 click 1   # → opens the Apple menu
```

The capture came out (2.5 MB of real desktop) and the click opened the menu. From there,
`System Settings…` sits in that same menu: the whole path to enabling the Accessibility
checkbox is clickable through this channel.

**Consequence**: permission bootstrap IS automatable, and there is no need for a separate
image per permission combination. Drive the first grant over VNC, and from then on the
guest can use its own tools.

Neither crabbox nor Peekaboo does this: crabbox does not mention TCC anywhere in its
documentation, and Peekaboo's says explicitly that you grant it by hand.

## The recipe

**Version requirement, and it is the trap that costs the most time**: you need **tart >= 2.29**
(tested with 2.35.0). In 2.28.1 `tart exec` computes the terminal size **always**, with an
unguarded `try!`, so it blows up when there is no TTY:

```
tart/Exec.swift:91: Fatal error: 'try!' expression unexpectedly raised an error:
failed to get terminal size: Inappropriate ioctl for device
ssh key injection failed
```

crabbox uses `tart exec` to inject the SSH key, so with tart 2.28.1 **the whole provider
fails to start from any non-interactive context**, which is exactly what it exists for. On
`main` it is wrapped in `if tty` and no longer happens. Not a bug to report: just update.

`.crabbox.yaml` at the repo root (see `crabbox.yaml.example` next to this file):

```yaml
jobs:
  mav-macos:
    provider: tart
    target: macos
    idleTimeout: 30m
    shell: true
    command: >
      export PATH=/usr/local/bin:/opt/homebrew/bin:$PATH &&
      mav doctor &&
      mav --profile mac run flows/smoke.yaml
    stop: always
```

The image is picked with `CRABBOX_TART_IMAGE` or `--tart-image`. Checked end-to-end with
`ghcr.io/cirruslabs/macos-tahoe-xcode:latest`: crabbox leases the VM, injects the key, syncs
the dirty checkout, runs, and releases the lease when done. When the command fails it leaves
a local failure bundle and suggests the exact `crabbox ssh` / `run --id` / `stop` invocations
to resume on the same lease.

### Who sets up what

The separation of responsibilities is crabbox's, not our invention. Its own documentation
pins it down: *"Crabbox owns the lease lifecycle, sync, execution and cleanup. The repository
owns the command string, package-manager setup, test environment"*.

| Layer | Who | What it sets up |
|---|---|---|
| Image | `scripts/build-mav-vm-image.sh`, once | mav, peekaboo, axcli |
| Machine | **crabbox** | lease, checkout sync, execution, cleanup |
| App | **mav** (`fixtures`) | the app's state before launching it |
| Permissions | whoever tests, against their app | TCC, see below |

Watch the word "fixture": crabbox does not use it for this. Its `warmup`/`prewarm` prepares
**the box**, not the app. App state is mav's job. And installing mav and the drivers **does
not go in a crabbox hook**: it goes in the image, or you reinstall it on every run.

### Why the image does NOT ship with permissions granted

Accessibility and Screen Recording could be baked in, because they go to the *tools* and are
the same every time. But the app under test asks for its own, Nokoru wants microphone,
calendar and Apple Events, and those change with every app. An image "with permissions"
would be **one image per app**, which is exactly what we do not want.

So the image ships tools and nothing else. Permissions are granted afterwards, against the
specific app, through the hypervisor channel described above. That keeps a single image
serving any app.

## What was actually tested, and where it stops

Run against NokoruMac in a macOS 26.0 VM:

| Step | Result |
|---|---|
| Image with mav + peekaboo + axcli | ✅ built and verified |
| crabbox: lease, sync, run, cleanup | ✅ end-to-end |
| App running inside the VM | ✅ |
| Fixture (`vacio` wipes the container) | ✅ the app launched showing the first-run onboarding |
| Accessibility + Event Synthesizing | ✅ **granted through the hypervisor channel, without touching the host** |
| Screen Recording | ❌ see below |
| `mav ui tree` / `mav capture` | ❌ blocked by the above |

### Three things that surfaced when running it

**1. A development-signed app does not launch in the VM.** NokoruMac carries
`embedded.provisionprofile` and restricted entitlements (iCloud, push) tied to a team and a
device list. In a clean VM, AMFI kills it with SIGKILL and no message. Validating UI does not
need those entitlements: re-signing ad-hoc (`codesign -f -s - --deep`) lets it launch. It is
a deliberate trade, iCloud and push are lost, and it has to be said, because you are no
longer testing the exact binary you ship.

**2. `mav ui tree` on macOS needs BOTH permissions, not just Accessibility.** `peekaboo see`
enumerates windows with ScreenCaptureKit, so without Screen Recording it fails with
`WINDOW_NOT_FOUND` even with Accessibility granted. Its own log says so: *"rejected onDemand
host … missing Screen Recording"*.

**3. Screen Recording cannot be granted to a CLI binary the normal way.** Unlike
Accessibility, where macOS registered `sshd-keygen-wrapper` on its own and flipping the
switch was enough, the Screen Recording pane comes up empty and its "+" button opens a
picker meant for `.app` bundles. Triggering the request from a process with no GUI registers
no entry.

Paths left to explore, none tested yet: grant to `Terminal.app` and drive from a terminal
inside the graphical session instead of over SSH; or capture the screen **from outside**
through the hypervisor's own VNC, which needs no permission at all. The visual evidence for
this test was obtained exactly that way.

**4. App-scoped capture also hits a Peekaboo v4 filter, not just TCC.** Running in the
graphical session, `peekaboo see --app nNokoru` enumerates the candidate windows and
discards them one by one, saying why:

```
Desktop observation target was not found: shareable window for nNokoru.
Candidates: #2 id=107 '<untitled>' 640x640 alpha=1.00 reason=layer != 0
```

`id=107` **is** the app's visible window. Peekaboo only accepts windows on layer 0, and
Nokoru's onboarding is a floating window. So: any app whose UI lives in a floating panel, a
HUD or a popover is out of reach for Peekaboo's per-app capture even with permissions in
perfect shape. `axcli snapshot` reads the tree of that same window without trouble (9
interactive elements, "Get started" button included), so the tree and the capture do not
fail for the same reason and do not get fixed the same way.

**5. Running `mav` over SSH leaves it in the wrong session.** A process launched from SSH is
not in the Aqua session: `screencapture` answers `could not create image from display`
because it sees no display at all. `sudo launchctl asuser 501 …` does enter the graphical
session, but then the TCC attribution the Peekaboo.app bridge provided is lost and the
answer becomes `Screen Recording permission is required`. Two different failures with the
same underlying cause, the identity of the responsible process, and neither is fixed by
granting `mav` more permissions.

**7. Per-app capture inside the VM only works through the Peekaboo.app bridge, and the
alternative failure is silent.** With Screen Recording granted to `axcli`, which is possible
even though the pane does not register the denied attempt: you add it with "+" and type the
path by hand, `axcli screenshot` reports success, writes the PNG and gets the window
measurements right (`Capturing window 640x640 at (192,52)`). What is inside the PNG is
**the desktop wallpaper**. Same with `--legacy`. Peekaboo, on the same window at the same
moment, returns the real content. The difference is that its CLI delegates to Peekaboo.app,
which lives in the graphical session, while axcli captures from its own process, and that
process, launched over SSH, has no graphical session. Control with TextEdit: same result, so
it is not the app under test.

A driver returning a plausible PNG instead of an error is worse than failing: that is why
axcli stays as an escape hatch (`--prefer-driver axcli`) for the floating windows Peekaboo
rejects, and not as the default path.

**6. `cg-pid` can fail silently on click, and here it did.** The risk noted as a hypothesis
in the plan got proven: `axcli click --strategy cg-pid "text=Get started"` reported success
(`cg-pid click pid=1740 wid=107 screen=(512,641)`) and the onboarding **did not advance**,
still on "Step 1 of 5". The same click with `--strategy ax` (AXPress) advanced to "Step 2 of
5" on the first try. For SwiftUI buttons AXPress is the right choice; for everything else,
`cg-pid` remains the one that does not steal focus. This is why `mav ui tap --verify`
exists: without checking the effect, a tap that did nothing gets reported as `ok`.

## What does not work yet

`crabbox run --artifact-glob` **rejects native macOS targets**, which is exactly the
mechanism you would use to pull `.mav/runs/<id>/` out of the VM. Tracked upstream in
[crabbox#1393](https://github.com/openclaw/crabbox/issues/1393). Meanwhile:

```sh
crabbox warmup --provider tart          # prints the lease slug
crabbox run --id <slug> -- mav run flows/smoke.yaml --profile mac-vm
rsync -a "$(crabbox ssh --id <slug> --print-target)":.mav/runs/ ./.mav/runs/
```

`mav`'s own output needs none of this: its `ok cmd=… k=v` line comes back over stdout as
is. What stays inside is the visual evidence.

## Two limits worth knowing before setting this up

- **At most 2 concurrent macOS VMs.** It is a limit of `Virtualization.framework` *and* of
  the macOS EULA; more RAM does not lift it. This is why crabbox's leasing goes from
  convenient to mandatory.
- **crabbox's `tart` provider does not expose `--audio`.** If what you validate needs a
  microphone, that VM will not have one even though tart itself can do it.
