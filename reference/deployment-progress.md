# Deployment progress — 2026-09-13

Session record of getting `setup.cmd` to run from other Windows PCs, and of the
defects found while deploying the first rooms.

## Status

The Linux provider and the Windows network share both work. Rooms can be applied
from a second PC. The RAPTOR, Dev-C++, Arduino IDE, Temurin, and TypeScript
defects found during the first Windows deployment are fixed in the catalog and
client.

| Component | State | Evidence |
| --- | --- | --- |
| `smbd` | running, enabled at boot | `systemctl is-active smbd` |
| `local-dist` | running, enabled at boot | `systemctl is-active local-dist` |
| HTTP API | serving | `GET /healthz` returns `{"status":"ok"}` |
| File endpoint | serving correct bytes | downloaded a package; SHA-256 matched the catalog |
| Range / HEAD support | working | `Accept-Ranges: bytes`, `206` on a range request |
| Traversal / listing refused | working | `/files/`, `../catalog.json`, and directory paths all `404` |
| SMB share | readable, authenticated, read-only | listed `setup.cmd` and `install.ps1`; bytes identical to originals |
| SMB rejects bad password | working | `NT_STATUS_LOGON_FAILURE` |
| SMB rejects writes | working | `NT_STATUS_ACCESS_DENIED` on `put` |

## Host changes (not in this repository)

These live on the Linux host. Only the systemd unit has a tracked counterpart
(`deployments/local-dist.service`, which targets the `/srv/software` production
layout — **not** what is running here).

### Samba

`/etc/samba/smb.conf` was given a read-only share and two global settings:

```ini
[software-scripts]
    comment = MTES read-only deployment scripts
    path = /home/dedibeat/projects/local-dist/scripts
    browseable = yes
    read only = yes
    guest ok = no
    valid users = dedibeat
```

```ini
# in [global]
   map to guest = never
   restrict anonymous = 2
```

Backups: `/etc/samba/smb.conf.bak-localdist`, `/etc/samba/smb.conf.bak2`.

A Samba password was set for the Linux user `dedibeat` with
`smbpasswd -a dedibeat`. **The password is deliberately not recorded here.**
It is separate from the sudo password.

Only `scripts/` is shared. `catalog.json`, `rooms.json`, `.git`, and the
installers are not exposed over SMB — they are already served by the HTTP
endpoint.

### systemd unit

`/etc/systemd/system/local-dist.service` runs the binary from the checkout:

```ini
ExecStart=/usr/local/bin/local-dist -listen :8080 -data /home/dedibeat/projects/local-dist/data
```

Binary installed at `/usr/local/bin/local-dist` via `go build -o`.
`ProtectHome` is intentionally **absent** here because the data directory lives
under `/home`; the production unit in `deployments/` keeps it.

Because this unit owns port 8080, `go run ./cmd/local-dist` fails with
"address already in use" unless the service is stopped first.

### Packages installed on the host

`smbclient` (to test the share) and `msitools` (to read MSI metadata).

## Client invocation

Run in an **elevated** Command Prompt on the target PC:

```cmd
net use Z: /delete /y >nul 2>&1 & net use Z: \\10.3.122.102\software-scripts /user:dedibeat * && Z:\setup.cmd room-302 -ServerUrl http://10.3.122.102:8080
```

`*` makes `net use` prompt for the share password instead of putting it on the
command line. Change `room-302` per room.

`install.ps1` is self-contained — it has no `$PSScriptRoot` or sibling-file
references — so it can also be called directly over UNC, which avoids the drive
letter entirely:

```cmd
net use \\10.3.122.102\software-scripts /user:dedibeat * && powershell -NoProfile -ExecutionPolicy Bypass -File \\10.3.122.102\software-scripts\install.ps1 room-302 -ServerUrl http://10.3.122.102:8080
```

## Findings

### Why Windows reported blocked guest access

Ubuntu's default `map to guest = bad user` maps a user name that does not exist
on Linux to the guest account. Windows sends its own logged-in account name,
which never exists on the Linux host, so the session became a guest session and
Windows refused it with:

> You can't access this shared folder because your organization's security
> policies block unauthenticated guest access.

`map to guest = never` makes Samba reject the unknown user with
`NT_STATUS_LOGON_FAILURE` instead, so Windows prompts for real credentials.
Verified both before and after on the host.

### `map to guest = never` is not visible in `testparm -s`

`Never` is Samba's built-in default, so `testparm -s` omits the line even after
it is set. Confirm it in the file instead:

```bash
grep -n 'map to guest' /etc/samba/smb.conf
```

### `cmd` never prompts for share credentials

Only File Explorer shows the "Enter network credentials" dialog. `net use` typed
in `cmd` fails immediately instead of prompting, unless the password is passed
or `*` is used. This is why "it didn't ask" was expected behaviour.

### Distinguishing the three SMB failures

| Server response | Windows error |
| --- | --- |
| `NT_STATUS_LOGON_FAILURE` | 1326 — "user name or password is incorrect" |
| `NT_STATUS_ACCESS_DENIED` | 5 — "Access is denied" (anonymous session) |
| `NT_STATUS_BAD_NETWORK_NAME` | 67 — "The network name cannot be found" (wrong share name) |

Error 5 is *not* a credentials problem; it means the session arrived
unauthenticated, usually from a non-elevated prompt or a stale cached credential.

### Samba logs are per client machine

Log files are named `log.<client-netbios-name>`. A client that reaches the
server creates a file there, which distinguishes a network problem from an
authentication problem. The default `log level = 0` records no authentication
failures, so a failed logon can leave an empty file. Raise it temporarily with:

```bash
sudo smbcontrol smbd debug 3
```

and revert with `sudo smbcontrol smbd debug 0`.

## Open items

### 1. `raptor` installs per-user and is invisible to other users — fixed

Read from the MSI's own tables (`msiinfo export wixedit_raptor.msi`):

```
Template    Intel;1033                     (32-bit package)
ProductName RAPTOR_Avalonia
ALLUSERS    (absent)          -> per-user install
INSTALLDIR  ProgramFilesFolder\RAPTOR_Avalonia
```

Because `ALLUSERS` is absent, `msiexec /qn` installs for whichever account ran
the elevated prompt. The Start Menu shortcut
(`Start Menu\Programs\RAPTOR Avalonia\RAPTOR.lnk`) and the uninstall entry go to
**that account only**, so the application cannot be found by another user and
does not appear in Start menu search. Verified as installed at
`C:\Program Files (x86)\RAPTOR_Avalonia\RAPTOR.EXE`.

The catalog also names the wrong folder:

| | Value |
| --- | --- |
| catalog `detect.path` | `C:\Program Files\RAPTOR\RAPTOR.exe` |
| actual | `C:\Program Files (x86)\RAPTOR_Avalonia\RAPTOR.EXE` |

Since the 32-bit template resolves `ProgramFilesFolder` to
`C:\Program Files (x86)` on 64-bit Windows.

The catalog now uses:

```json
"install": { "args": ["ALLUSERS=1"] },
"detect":  { "type": "file", "path": "C:\\Program Files (x86)\\RAPTOR_Avalonia\\RAPTOR.EXE" }
```

`ALLUSERS=1` makes the install machine-wide so every user sees it. Note the
existing per-user copy shares the same ProductCode
(`{62E8746B-9FBB-4E5A-AA2C-0A6B787C7338}`), so installing with `ALLUSERS=1` may
return error 1638 until the per-user copy is removed. The Windows client also
repairs the all-users `RAPTOR Avalonia\RAPTOR.lnk` shortcut. A one-time `-Force`
run may be needed to migrate an existing per-user copy.

### 2. Dev-C++ detect path names the wrong folder — fixed

The official NSIS script uses `InstallDir $PROGRAMFILES\Embarcadero\Dev-Cpp`
and creates `Embarcadero Dev-C++\Dev-C++.lnk` in the all-users Start menu. This
32-bit installer therefore resolves to:

```
C:\Program Files (x86)\Embarcadero\Dev-Cpp\devcpp.exe
```

The catalog previously omitted the `Embarcadero\` directory. Its detection
path now matches the installer, and the client repairs that all-users shortcut
if Windows Search does not pick up the publisher-created one.

### 3. `arduino-ide` installs per-user and names the wrong folder — fixed

```
ALLUSERS          = 2
MSIINSTALLPERUSER = 1
APPLICATIONFOLDER = ProgramFiles64Folder\arduino-ide  ->  C:\Program Files\arduino-ide
```

That property combination defaults to a per-user install. Windows confirmed the
existing copy at `%LOCALAPPDATA%\Programs\arduino-ide\Arduino IDE.exe`, while the
MSI Directory table defines its machine target as
`C:\Program Files\arduino-ide\Arduino IDE.exe`. The catalog now passes
`ALLUSERS=1 MSIINSTALLPERUSER=`, checks the machine target, and repairs an
all-users shortcut. Windows Installer returned success without changing the
context of an already-installed per-user copy, so that old copy must be removed
before reinstalling. The Arduino catalog entry now supplies its MSI product code,
and SETUP performs that remove-and-reinstall migration automatically whenever
the machine-wide executable is missing.

### 4. Remaining `detect` paths need wider Windows coverage

The Room 208 file paths were checked on Windows. Other rooms still contain EXE
and ZIP packages that have not been installed on this client, so their paths
need confirmation during the first room deployment.

### 5. "Completed successfully" did not mean the software was usable — fixed

The client now retries verification briefly, records a package failure, and
continues with the remaining packages when post-install detection still fails.
The overall run returns exit code `1` if any package failed. Command detection
also checks an executable's exit code. This caught TypeScript 7's broken
launcher: the main npm tarball requires a separate Windows x64 compiler package
that the old offline install did not provide. That platform package is now part
of every TypeScript room plan, and `tsc.cmd --version` verifies it.

### 6. Rooms are incomplete

`room-208` and `room-302` both report `complete: false` with **4 pending
requirement groups**. Applying a room installs only its listed packages and does
not finish the room. This is correct behaviour and must not be papered over by
marking rooms complete.

### 7. The server address is DHCP-assigned

`10.3.122.102` is dynamic. The share path and `-ServerUrl` both hard-code it, so
a lease change breaks every client until the commands are updated. Either set a
static lease or create the planned `mtes-pkg` DNS name.

### 8. The host is a laptop that suspends when the lid closes

`systemd-logind` uses the default `HandleLidSwitch=suspend`, which applies on AC
power too. Closing the lid stops the share and the HTTP API mid-deployment.
Keep the lid open, or set `HandleLidSwitch=ignore` in
`/etc/systemd/logind.conf`.

### 9. A share credential is committed in git history

`oneline.md` contained the plaintext Samba password and was committed. The
working copy has been redacted to a placeholder, **but the value remains in the
history of commits `a571c2e` and `c5a3791`.** Rotate it with
`sudo smbpasswd dedibeat` and update the client command; rewriting history is a
separate decision.

## Not verified

- Room 208's installed paths and TypeScript compiler were checked on Windows.
  A clean-machine installation of the complete merged plan has not been run.
- Rooms were not re-applied after the `map to guest = never` change.

## Suggested next steps

1. Fetch the new TypeScript Windows package, then restart the provider so the
   merged catalog and room plans load.
2. Run the affected rooms once with `-Force`; collect the automatic client log
   if a package still does not appear in Start search.
3. Rotate the share password (open item 9).
4. Confirm the remaining `detect` paths on a real PC (open item 4).
5. Consider serving the client script over HTTP so the share credential is not
   needed at all; this requires a new route in `internal/provider/http.go`,
   because `/files/` serves only files named in the catalog.
