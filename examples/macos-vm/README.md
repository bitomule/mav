# Running `mav` against a macOS app inside a disposable VM

This is where the measurements behind `vm: true` live. **The feature itself needs none of
this**: put `vm: true` next to `target_kind: macos`, run `mav setup --install vm` once and
`scripts/build-mav-vm-image.sh` once, and mav leases the machine, ships the app across,
drives it and hands the machine back on its own. See the README's *Running the app in a
disposable VM*.

What follows is the record of what was tried and what it cost, kept because most of it
contradicts the guides on the subject and rediscovering it is expensive.

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

## The version floor, and it is the trap that costs the most time

You need **tart >= 2.29** (measured working with 2.32.1). In 2.28.1 `tart exec` computes
the terminal size **always**, with an unguarded `try!`, so it blows up when there is no
TTY:

```
tart/Exec.swift:91: Fatal error: 'try!' expression unexpectedly raised an error:
failed to get terminal size: Inappropriate ioctl for device
ssh key injection failed
```

The lease tool uses `tart exec` to inject the SSH key, so with 2.28.1 **the whole
provider fails to start from any non-interactive context**, which is exactly what it
exists for. Nothing in that message points at the version, which is why `mav doctor`
reports `vm_tooling=outdated` and `mav setup --install vm` upgrades it rather than leaving you
to read a stack trace about ioctls.

### Who sets up what

| Layer | Who | What it sets up |
|---|---|---|
| Image | `scripts/build-mav-vm-image.sh`, once | mav, cua-driver, axcli, mitmproxy, the driver's TCC grants |
| Machine | **mav** (`vm: true`) | lease, checkout and bundle sync, execution, evidence pull, release |
| App | **mav** (`fixtures`) | the app's state before launching it |
| Permissions the app itself asks for | whoever tests, against their app | TCC, see below |

Installing mav and the drivers goes **in the image**, not in a per-run hook, or you
reinstall them on every run. The image's own `.zshenv` does not put `~/.local/bin` on the
PATH, which is where the driver installer leaves its binary; mav exports the full PATH on
every remote call rather than depending on which shell the guest picks.

### Why the image does NOT ship with permissions granted

Accessibility and Screen Recording could be baked in, because they go to the *tools* and are
the same every time. But the app under test asks for its own, Nokoru wants microphone,
calendar and Apple Events, and those change with every app. An image "with permissions"
would be **one image per app**, which is exactly what we do not want.

So the image ships tools and nothing else. Permissions are granted afterwards, against the
specific app, through the hypervisor channel described above. That keeps a single image
serving any app.

## What was actually tested, and where it stops

Two rounds, both against NokoruMac in a macOS 26.0 VM. The first used Peekaboo and got
stuck; the second, after cua-driver replaced it and its grants were baked into the image,
went the whole way through `mav` alone.

| Step | Peekaboo, first round | cua-driver, with `vm: true` |
|---|---|---|
| Image with mav and the drivers | ✅ | ✅ |
| Lease, sync, run, cleanup | ✅ | ✅ driven by `mav` itself |
| App running inside the VM | ✅ | ✅ after ad-hoc re-signing, see below |
| Accessibility + Screen Recording | ✅ granted through the hypervisor channel | ✅ baked into the image |
| `mav ui tree` | ❌ blocked by Screen Recording | ✅ 76 nodes |
| `mav capture` | ❌ same | ✅ real window content, in the local run dir |
| `mav ui tap` | not reached | ✅ onboarding advanced |
| `mav network start/stop` | not reached | ✅ proxy set and restored **inside** the VM |
| `mav evidence step` / `report` | not reached | ✅ |
| `mav evidence start` (video) | not reached | ✅ 1m31s of H.264, through the driver daemon |
| `mav stop` | not reached | ✅ machine handed back, VM gone |

### What surfaced when running it

**1. A development-signed app does not launch in the VM.** NokoruMac carries
`embedded.provisionprofile` and restricted entitlements (iCloud, push) tied to a team and a
device list. In a clean VM, AMFI kills it with SIGKILL and no message; three commands later
the symptom is "the app is not running", which points at everything except the signature.
Re-signing ad-hoc (`codesign -f -s - --deep`) lets it launch, and validating UI does not
need those entitlements. `mav` does this to the guest's copy and reports `resigned=adhoc`
on `open`: it is a deliberate trade, iCloud and push go with it, and it has to be said,
because you are no longer testing the exact binary you ship.

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

## The evidence channel, and why it is mav's

`crabbox run --artifact-glob` **rejects native macOS targets**, which is exactly the
mechanism you would use to pull `.mav/runs/<id>/` out of the VM. Tracked upstream in
[crabbox#1393](https://github.com/openclaw/crabbox/issues/1393). So mav rsyncs the run
directory back itself, over the lease's own SSH, after every command.

It does it per command and not once at the end because an agent driving mav command by
command never reaches anything that would be "the end", and evidence it cannot read until
some later command happens to sync is evidence it reasons about stale.

## Two limits worth knowing before setting this up

- **At most 2 concurrent macOS VMs.** It is a limit of `Virtualization.framework` *and* of
  the macOS EULA; more RAM does not lift it. This is why leasing goes from convenient to
  mandatory, and why mav hands the machine back on `mav stop`, at the end of a flow, and
  on an idle timeout rather than trusting anyone to remember.
- **The `tart` provider does not expose `--audio`.** If what you validate needs a
  microphone, that VM will not have one even though tart itself can do it.
- **Video has exactly one working path in here, and three that look like they should.**
  `screencapture -v` sees no display over SSH. The driver's persistent `recording start`
  captures per-action stills and its video flag does nothing there, while
  `recording render` refuses without an mp4 only the other path produces. The
  hypervisor's own desktop recording answers *"artifacts video currently requires
  target=linux or native Windows desktop capture"*. What works is holding an MCP session
  open against the driver daemon for the length of the recording, which is what mav does.
