# Windows room setup

Run the following in an **elevated Command Prompt** on the new Windows PC.
The `*` makes Windows prompt for the Samba password without putting it on the
command line.

## Recommended: map the script share

```cmd
net use Z: /delete /y >nul 2>&1 & net use Z: \\10.3.122.102\software-scripts /user:dedibeat RoomDeploy-2026 && Z:\setup.cmd room-302 -ServerUrl http://10.3.122.102:8080
```

Change `room-302` for a different room. Change `10.3.122.102` if the server's
address changes. The first `net use` only removes an old mapping; its failure
is intentionally ignored. The `&&` prevents setup from running when share
authentication fails.

## Direct UNC invocation

This avoids assigning a drive letter:

```cmd
net use \\10.3.122.102\software-scripts /user:dedibeat * && powershell -NoProfile -ExecutionPolicy Bypass -File \\10.3.122.102\software-scripts\install.ps1 room-302 -ServerUrl http://10.3.122.102:8080
```

Setup writes a transcript to
`C:\ProgramData\MTES\LocalDist\logs`. To choose a specific log file, append:

```cmd
-LogPath C:\Temp\room-302-setup.log
```

## Stale credentials

On a re-imaged PC, clear the old credential and connection first:

```cmd
cmdkey /delete:10.3.122.102 >nul 2>&1 & net use \\10.3.122.102\software-scripts /delete /y >nul 2>&1 & net use \\10.3.122.102\software-scripts /user:dedibeat * && powershell -NoProfile -ExecutionPolicy Bypass -File \\10.3.122.102\software-scripts\install.ps1 room-302 -ServerUrl http://10.3.122.102:8080
```

If the Command Prompt is not elevated, close it and choose **Run as
administrator**. The Windows client requires elevation.

Room 302 is still incomplete: it has four pending requirement groups. This
command installs its eight approved packages; it does not finish the room's
remaining requirements.