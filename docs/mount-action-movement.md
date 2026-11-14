# Mount and action movement controls

This change set is prepared on the isolated `codex/mount-action-recovery-20261113-updated` branch, based on upstream `b02daec4a2321640b0fc04167d01293f813e7ce7`. It keeps the GX EP5.4 protocol and local runtime separate from the Shaiya2 CORE implementation.

## Behavior

- The world server owns timed skill and mount cast deadlines. A `0x0501` packet cannot clear a pending cast: changed coordinates, vertical changes, and nonzero movement modes are rejected until the deadline. Rejections send a bounded position correction (at most one per 100 ms) and a throttled log entry.
- While a skill cast is pending, the server also blocks new attacks and skills, stops the current auto-attack, and refuses locomotion `0x0506` motions. The client suppresses local movement prediction during a cast and keeps the cast animation when it receives an authoritative position correction.
- The server's existing weapon recovery deadline also rejects movement. The client now holds movement through the matching local attack action and the server-reported attack-speed interval, avoiding repeated rejected movement and visible position snaps.
- The client keeps the camera's right-drag independent from a fixed travel or combat heading. Right-drag can still steer while the player is moving freely.
- Backward movement on a mount uses the native walk animation and preserves the rider's forward heading. Mounted jump uses the matching `Vehicle/ANI/*_jump.ANI` when the mount model provides it; remote mounted characters use the same motion flag.
- Mount reuse is one timer for equipment bag 0, slot 13. The server starts it when dismounting or replacing the active mount, using the last mounted item's `ReqIg` value. Mounting does not start or reset it, and changing the item in slot 13 does not evade the remaining time. The client shades only that equipment slot and displays the server-provided milliseconds.
- The admin chat commands `/getitem <type> <id>` and `/mount [seconds]` remain available for the local test server. `/getitem` searches bags 1–5 for the first empty slot. `/mount N` starts a server-timed cast; `/mount 0` activates instantly.
- Right-clicking an equipped inventory item moves it to the first available slot in bags 1–5. Right-clicking an item in a regular bag keeps the existing equip behavior.

## Wire compatibility

GX uses two optional response extensions in this branch: four milliseconds bytes on `0x0216` for mount cooldown, and four seconds bytes on a mount-only `0x0510` cast-start. The GX client decoder tolerates the original shorter packets. These extensions should be reviewed before sending this branch to an upstream server that does not implement them.

## Validation and limits

- `gofmt` and `git diff --check` completed.
- Windows/amd64 builds succeeded with Go 1.27.3; server build used `CGO_ENABLED=0`, client build used LLVM-MinGW x64 with `CGO_ENABLED=1`.
- Automated tests were not run. The binaries were installed to the local GX test packages, and login/world are listening on `127.0.0.1:30800` and `127.0.0.1:30810`; an in-game gameplay pass is still needed.
- Timed skills with nonzero server `ReadyTime` are authoritatively movement-locked through their cast deadline. A zero-`ReadyTime` skill resolves immediately on the server; its release animation is locally movement-locked, but no arbitrary server-side release delay was added. If upstream requires movement to remain blocked after an instant skill's impact, the authoritative duration needs to be derived from verified skill action data rather than guessed.
- Mount reverse movement and jump animation selection compile, but their appearance and timing still need a visual in-game check across the available mount models.

## Reproducing the Windows build

From the repository root, with the local Go 1.27.3 toolchain and LLVM-MinGW x64 available:

```powershell
$env:GOOS='windows'
$env:GOARCH='amd64'
$env:GOAMD64='v1'
$env:CGO_ENABLED='0'
go build -trimpath -o build/gx_world.exe ./server/cmd/gx_world
```

For the client, set `CGO_ENABLED=1`, set `CC` to `x86_64-w64-mingw32-gcc.exe`, put the LLVM-MinGW `bin` directory on `PATH`, and build `./cmd/shaiya` to `Shaiya GX.exe`.
