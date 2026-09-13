# Deployment progress — 2026-09-13

Session record of getting `setup.cmd` to run from other Windows PCs, and of the
defects found while deploying the first rooms.

## Status

The Linux provider and the Windows network share both work. Rooms can be applied
from a second PC. Two catalog defects were found and are **not yet fixed** — see
[Open items](#open-items).

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

### 1. `raptor` installs per-user and is invisible to other users

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

Proposed change to `data/catalog.json`:

```json
"install": { "args": ["ALLUSERS=1"] },
"detect":  { "type": "file", "path": "C:\\Program Files (x86)\\RAPTOR_Avalonia\\RAPTOR.EXE" }
```

`ALLUSERS=1` makes the install machine-wide so every user sees it. Note the
existing per-user copy shares the same ProductCode
(`{62E8746B-9FBB-4E5A-AA2C-0A6B787C7338}`), so installing with `ALLUSERS=1` may
return error 1638 until the per-user copy is removed.

### 2. `arduino-ide` detect path names the wrong folder

```
ALLUSERS          = 2                                      (per-machine when elevated - fine)
APPLICATIONFOLDER = ProgramFiles64Folder\arduino-ide  ->  C:\Program Files\arduino-ide
```

The catalog says `C:\Program Files\Arduino IDE\Arduino IDE.exe`. The application
is installed machine-wide and visible, but `detect` never matches, so it is
reinstalled on every run. The correct folder is `arduino-ide`; **the executable
filename inside has not been confirmed.**

### 3. `detect` paths for non-MSI packages are unverified

`detect.path` was only checkable for MSI packages, by reading their Directory
tables. The `exe`, `zip`, and `npm` packages cannot be verified from Linux. The
same wrong-path defect may exist in any of them. A wrong `detect` path does not
break installation, but it causes silent reinstallation on every run.

### 4. "Completed successfully" does not mean the software is usable

`install.ps1` prints success when every installer exits `0`, `1641`, or `3010`.
That confirms the installer ran, not that the application is registered
machine-wide, visible in the Start menu, or on `PATH`. The `raptor` case is
exactly this: the run was successful and the application was still not findable.

### 5. Rooms are incomplete

`room-208` and `room-302` both report `complete: false` with **4 pending
requirement groups**. Applying a room installs only its listed packages and does
not finish the room. This is correct behaviour and must not be papered over by
marking rooms complete.

### 6. The server address is DHCP-assigned

`10.3.122.102` is dynamic. The share path and `-ServerUrl` both hard-code it, so
a lease change breaks every client until the commands are updated. Either set a
static lease or create the planned `mtes-pkg` DNS name.

### 7. The host is a laptop that suspends when the lid closes

`systemd-logind` uses the default `HandleLidSwitch=suspend`, which applies on AC
power too. Closing the lid stops the share and the HTTP API mid-deployment.
Keep the lid open, or set `HandleLidSwitch=ignore` in
`/etc/systemd/logind.conf`.

### 8. A share credential is committed in git history

`oneline.md` contained the plaintext Samba password and was committed. The
working copy has been redacted to a placeholder, **but the value remains in the
history of commits `a571c2e` and `c5a3791`.** Rotate it with
`sudo smbpasswd dedibeat` and update the client command; rewriting history is a
separate decision.

## Not verified

- **No Windows machine was available.** The `net use` and `-File` invocation,
  and every installer's behaviour under `msiexec /qn` and NSIS `/S`, are
  unverified end to end. The Linux side of the download path is verified
  (checksums match); installation behaviour is not.
- Rooms were not re-applied after the `map to guest = never` change.

## Suggested next steps

1. Decide on the `raptor` and `arduino-ide` catalog corrections (open items 1
   and 2), then restart the provider so the catalog reloads.
2. Rotate the share password (open item 8).
3. Confirm the remaining `detect` paths on a real PC (open item 3).
4. Consider serving the client script over HTTP so the share credential is not
   needed at all; this requires a new route in `internal/provider/http.go`,
   because `/files/` serves only files named in the catalog.
