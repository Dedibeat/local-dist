# MTES Local Installer Server

This project lets one Linux computer store software installers for many Windows computers on the same local network.

For example, a technician can run one command on a computer in Room 302:

```cmd
\\mtes-pkg\software\scripts\setup.cmd room-302
```

The computer asks the server which programs Room 302 needs, downloads them, checks that the files are correct, and installs them silently.

```text
Linux server                         Windows computer
----------------                    ----------------
Room 302 package list  ----------->  Read the list
Installer files        ----------->  Check and install them
```

The Windows computers do not need Python, Node.js, Chocolatey, or a custom background program. They use Command Prompt and PowerShell, which are already included with Windows.

## What is included?

- A small web server written in Go
- A package list in `data/catalog.json`
- A room list in `data/rooms.json`
- A Windows installation script
- A Samba configuration example for the `\\mtes-pkg\software` network share
- Docker and systemd options for keeping the server running

This workspace currently contains Node.js LTS, Git for Windows, Visual Studio Code, w64devkit, Python, Eclipse Temurin JDK 21, 7-Zip, Google Chrome, Arduino IDE, GNU Octave, Anaconda, Code::Blocks, Embarcadero Dev-C++, Android Studio, Flutter SDK, R, RStudio Desktop, MongoDB Community Server, OpenSSH Client, Docker Desktop, TypeScript, .NET Desktop Runtime, and RAPTOR. Large installer files are ignored by Git. After making a new clone, restore the approved files and verify their checksums with:

```bash
go run ./cmd/fetch-packages -data ./data
```

## Words used in this manual

| Word | Meaning |
| --- | --- |
| Server | The Linux computer that stores and sends installers |
| Client | A Windows computer that receives the software |
| Package | One installer and its information, such as 7-Zip version 24.09 |
| Catalog | The list of all packages on the server |
| Room plan | The packages that should be installed in one room |
| IP address | A computer's address on the network, such as `192.168.1.50` |
| SHA-256 | A file fingerprint used to detect a broken or changed download |
| Silent install | Installing without asking the user to click through setup screens |

## Before you start

You need:

- One Ubuntu or Debian Linux server
- Windows computers on the same network
- Administrator access on the Linux and Windows computers
- Go 1.23 or newer on the Linux server
- This project copied or cloned onto the Linux server

Commands beginning with `sudo` ask for the Linux administrator password. Run Linux commands in Terminal. Run Windows commands in an **Administrator Command Prompt**.

The provider uses unauthenticated HTTP by default and is intended for a trusted, access-controlled LAN. SHA-256 checks detect file corruption, but the catalog arrives over the same connection and does not authenticate the server. Use an HTTPS endpoint with a certificate trusted by the Windows clients when the network cannot be trusted, and set `-ServerUrl` accordingly.

## Part 1: Test the provider

Open Terminal inside this project and run:

```bash
go run ./cmd/fetch-packages -data ./data
go run ./cmd/local-dist
```

The first command downloads any missing catalog packages. It stops if an existing or downloaded file has the wrong SHA-256 fingerprint.

Leave that Terminal open. Open a second Terminal and run:

```bash
curl http://localhost:8080/healthz
```

You should see:

```json
{"status":"ok"}
```

Check the example Room 302 plan:

```bash
curl http://localhost:8080/api/v1/rooms/room-302
```

The response should show Visual Studio Code, mark it as available, and explain that the room plan is still incomplete:

```json
{
  "id": "room-302",
  "complete": false,
  "packages": [
    {
      "id": "vscode",
      "available": true
    }
  ]
}
```

The real response contains more details than this shortened example.

Press `Ctrl+C` in the first Terminal to stop the test server.

## Part 2: Install the provider on Linux

### 1. Build the program

From the project directory, run:

```bash
go build -o local-dist ./cmd/local-dist
```

### 2. Create the server folders and account

```bash
sudo useradd --system --home /nonexistent --shell /usr/sbin/nologin local-dist
sudo mkdir -p /srv/software
sudo install -m 0755 local-dist /usr/local/bin/local-dist
sudo cp data/catalog.json data/rooms.json /srv/software/
sudo cp -r data/packages scripts /srv/software/
sudo chmod -R a+rX /srv/software
```

If Linux says the `local-dist` user already exists, that is okay. Continue with the next command.

The final folder layout will be:

```text
/srv/software/
├── catalog.json       information about every package
├── rooms.json         packages assigned to each room
├── packages/          MSI, EXE, and ZIP files
└── scripts/           Windows setup scripts
```

### 3. Start it automatically with systemd

```bash
sudo cp deployments/local-dist.service /etc/systemd/system/
sudo systemctl daemon-reload
sudo systemctl enable --now local-dist
sudo systemctl status local-dist
```

The status should say `active (running)`. Press `q` to leave the status screen.

Find the server's IP address:

```bash
hostname -I
```

Use the first local address shown. This manual uses `192.168.1.50` as an example. Replace it with your real address.

Test the server from another computer:

```text
http://192.168.1.50:8080/healthz
```

If the Linux firewall is active, allow the provider port:

```bash
sudo ufw allow 8080/tcp
```

## Part 3: Create the Windows network share

Samba lets Windows open a Linux folder using a path such as `\\mtes-pkg\software`.

### 1. Install Samba

```bash
sudo apt update
sudo apt install samba
```

### 2. Create a read-only download account

```bash
sudo useradd -M -s /usr/sbin/nologin deploy
sudo smbpasswd -a deploy
```

Choose a password when asked. Windows technicians will use this account to open the share. Do not use an important personal password.

### 3. Add the share configuration

Open the Samba configuration:

```bash
sudo nano /etc/samba/smb.conf
```

Go to the bottom and add:

```ini
[software]
    path = /srv/software
    browseable = yes
    read only = yes
    guest ok = no
    valid users = deploy
```

The share on its own is not enough. Samba's default `map to guest = bad user`
turns the technician's Windows user name — which does not exist on Linux — into a
guest session, and Windows then refuses it with "your organization's security
policies block unauthenticated guest access". Add these two lines to the
`[global]` section near the top of the same file:

```ini
   map to guest = never
   restrict anonymous = 2
```

`map to guest = never` makes Samba reject an unknown user name outright, so
Windows asks for the share password instead of trying guest access.

Save with `Ctrl+O`, press `Enter`, and close Nano with `Ctrl+X`.

Check the configuration and start Samba:

```bash
sudo testparm
sudo systemctl enable --now smbd
sudo systemctl restart smbd
```

If the Linux firewall is active, allow Samba:

```bash
sudo ufw allow Samba
```

### 4. Test from Windows

Open File Explorer and enter this in the address bar:

```text
\\192.168.1.50\software
```

Enter the username `deploy` and the Samba password you created. You should see `catalog.json`, `rooms.json`, `packages`, and `scripts`.

Later, your network administrator can create the DNS name `mtes-pkg`. You can then use `\\mtes-pkg\software` instead of the IP address.

## Part 4: Add another installer

This example uses an imaginary file named `7z2409-x64.exe`. Use the real filename and version you downloaded.

### 1. Copy the installer

From the project directory on Linux, run:

```bash
sudo ./scripts/add-package.sh ~/Downloads/7z2409-x64.exe /srv/software/packages/7zip
```

The command prints a SHA-256 value similar to this:

```text
SHA256: 9b3a...64 hexadecimal characters in total...7f1c
```

Copy that complete value. You need it in the next step.

The helper makes the installer readable by the provider and refuses to overwrite an existing filename. Use a new, versioned filename for updates.

### 2. Add the package to the catalog

Open the catalog:

```bash
sudo nano /srv/software/catalog.json
```

Add this package object inside the existing `packages` list. Do not delete the packages already there. Put a comma between package objects. Replace the example version, filename, and SHA-256 with the real values.

```json
{
  "id": "7zip",
  "name": "7-Zip",
  "version": "24.09",
  "type": "exe",
  "source": "packages/7zip/7z2409-x64.exe",
  "sha256": "PASTE_THE_64_CHARACTER_SHA256_HERE",
  "install": {
    "args": ["/S"]
  },
  "detect": {
    "type": "file",
    "path": "C:\\Program Files\\7-Zip\\7z.exe"
  }
}
```

JSON is strict. Keep every quote, comma, bracket, and brace in the correct place. JSON does not allow comments.

The `/S` value tells this particular installer to run silently. Other installers may use `/quiet`, `/silent`, `/VERYSILENT`, or another value. Check the software publisher's instructions before adding an EXE installer.

### 3. Assign the package to a room

Open the room list:

```bash
sudo nano /srv/software/rooms.json
```

Find Room 302 and add the new package ID to its existing `packages` line. Keep the existing `vscode` entry:

```json
"packages": ["vscode", "7zip"]
```

Do not replace the other rooms or Room 302's `pending` list. The text `7zip` must exactly match the `id` in `catalog.json`. Keep `"complete": false` while the room still has pending requirements.

### 4. Restart and check the provider

```bash
sudo systemctl restart local-dist
sudo systemctl status local-dist
```

Check Room 302:

```bash
curl http://localhost:8080/api/v1/rooms/room-302
```

Find `"available": true` in the response. If it says `false`, the `source` filename in `catalog.json` does not match the real file.

## Part 5: Install the room software on Windows

Open **Command Prompt as Administrator**:

1. Open the Start menu.
2. Type `cmd`.
3. Right-click **Command Prompt**.
4. Choose **Run as administrator**.

Run the setup command. Before DNS is configured, use the server IP in both places:

```cmd
\\192.168.1.50\software\scripts\setup.cmd room-302 -ServerUrl http://192.168.1.50:8080
```

Windows will:

1. Ask the server for Room 302's package list.
2. Download each installer.
3. Compare its SHA-256 fingerprint with the catalog.
4. Skip software that the detection rule says is already installed.
5. Install the remaining software.
6. Verify the expected program file after every installation.

After DNS is configured, the shorter command is:

```cmd
\\mtes-pkg\software\scripts\setup.cmd room-302
```

Downloaded files are cached in:

```text
C:\ProgramData\MTES\LocalDist\cache
```

The cache saves time when you run the setup again.

Each run also saves a PowerShell transcript under:

```text
C:\ProgramData\MTES\LocalDist\logs
```

The transcript records the room request, detection paths and results, installer
exit codes, and any repaired Start-menu shortcuts. To choose a specific log
file, pass `-LogPath`:

```cmd
\\mtes-pkg\software\scripts\setup.cmd room-302 -LogPath C:\Temp\room-302-setup.log
```

The provider does not receive logs automatically; copy or send the resulting
file to the operator when a Windows installation needs investigation.

Detection checks whether a file exists; it does not compare installed versions.
After an installer exits successfully, SETUP retries its verification briefly
and records a failure if the expected file or command is still unavailable.
Package failures are logged and skipped so the remaining room packages can
continue. SETUP returns exit code `1` when any package fails. To update or
reinstall software that is already detected, add `-Force`:

```cmd
\\mtes-pkg\software\scripts\setup.cmd room-302 -Force
```

This reinstalls every package in the selected room plan. The script returns exit code `0` on success, `3010` when installation succeeded but Windows needs a restart, and `1` on failure. Configured PATH entries are also repaired for already-installed packages.

Command-line packages can use a `command` detection rule. SETUP runs the command
and considers it installed only when it exits successfully. TypeScript uses
`tsc.cmd --version`, which also verifies that its Windows compiler package is
present; a leftover launcher file by itself is not accepted as a working install.

Arduino IDE, RAPTOR, and Dev-C++ are given all-users Start-menu shortcuts by the
client. Arduino IDE and RAPTOR are also installed in machine-wide MSI mode. For
an older copy of Arduino IDE, SETUP removes the existing MSI registration before
installing it machine-wide because Windows Installer cannot change that context
in place. For an older RAPTOR copy that was created per-user, run once with
`-Force`. If Windows Installer reports error 1638 for RAPTOR, remove that old
copy from **Installed apps** and run SETUP again.

## Package types

### MSI installer

MSI files already support Windows silent installation. Extra arguments are optional:

```json
{
  "id": "example-msi",
  "name": "Example MSI Program",
  "version": "1.0",
  "type": "msi",
  "source": "packages/example/setup.msi",
  "sha256": "PUT_THE_REAL_SHA256_HERE",
  "install": {
    "args": []
  }
}
```

### EXE installer

Each software publisher chooses its own silent arguments:

```json
"type": "exe",
"install": {
  "args": ["/VERYSILENT", "/NORESTART"]
}
```

### ZIP package

ZIP packages are extracted to a folder:

```json
"type": "zip",
"install": {
  "destination": "C:\\Tools\\MyProgram"
}
```

### Portable file

A portable package copies one file into a folder:

```json
"type": "portable",
"install": {
  "destination": "C:\\Tools\\MyProgram"
}
```

## Add more rooms

Put another room object inside the `rooms` list. Separate room objects with a comma:

```json
{
  "schemaVersion": 1,
  "rooms": [
    {
      "id": "room-302",
      "name": "Room 302",
      "packages": ["7zip"]
    },
    {
      "id": "room-303",
      "name": "Room 303",
      "packages": ["7zip", "vscode-portable"]
    }
  ]
}
```

Restart the provider after changing `catalog.json` or `rooms.json`:

```bash
sudo systemctl restart local-dist
```

## Common problems

### The provider does not start

Read its latest messages:

```bash
sudo journalctl -u local-dist -n 50 --no-pager
```

The message normally points to a JSON error, a bad SHA-256 value, a repeated ID, or a room that names a package missing from the catalog.

### Windows cannot open the network share

Try the IP address first:

```text
\\192.168.1.50\software
```

Then check Samba on Linux:

```bash
sudo testparm
sudo systemctl status smbd
```

Also confirm that Windows and Linux are connected to the same network.

### Windows says guest access is blocked

The message "your organization's security policies block unauthenticated guest
access" means Samba mapped the unknown Windows user name to the guest account.
Check that the global setting is present:

```bash
grep -n 'map to guest' /etc/samba/smb.conf
```

It must show `map to guest = never`. `testparm -s` does not print this line,
because `never` is Samba's built-in default.

### Windows says the user name or password is incorrect

`cmd` and `net use` do not prompt for share credentials; only File Explorer does.
Pass the account on the command line and let it ask for the password:

```cmd
net use Z: \\192.168.1.50\software /user:deploy *
```

If the account was already cached with a wrong password, remove it first:

```cmd
net use Z: /delete /y
cmdkey /delete:192.168.1.50
```

Use those targeted forms rather than `net use * /delete /y`, which removes every
mapped drive on the PC.

### Windows cannot contact port 8080

Open this address in a Windows browser:

```text
http://192.168.1.50:8080/healthz
```

If it does not show `{"status":"ok"}`, check the Linux service and firewall:

```bash
sudo systemctl status local-dist
sudo ufw status
```

### The package says `available: false`

Check that the source in `catalog.json` exactly matches the package location and filename. Linux filenames use uppercase and lowercase letters differently, so `Setup.exe` and `setup.exe` are different names.

### The checksum does not match

The downloaded file is different from the file described by the catalog. Run this on Linux:

```bash
sha256sum /srv/software/packages/7zip/7z2409-x64.exe
```

Copy the new value into `catalog.json`, but only after confirming that the installer came from a trusted source.

### An EXE opens setup windows or waits forever

Its silent arguments are probably wrong. Read the software publisher's deployment documentation and update `install.args` in `catalog.json`.

### Arduino IDE, RAPTOR, or Dev-C++ is installed but missing from Start search

The catalog uses the verified executable locations below and repairs a common
all-users Start-menu shortcut on every setup run:

| Package | Executable |
| --- | --- |
| Arduino IDE | `C:\Program Files\arduino-ide\Arduino IDE.exe` |
| RAPTOR | `C:\Program Files (x86)\RAPTOR_Avalonia\RAPTOR.EXE` |
| Dev-C++ | `C:\Program Files (x86)\Embarcadero\Dev-Cpp\devcpp.exe` |

Restart `local-dist`, then run the room setup again. If the executable is
already present, the shortcut repair runs without `-Force`; use `-Force` for
the RAPTOR per-user-to-machine-wide migration described above. If it still
does not appear, inspect the log named by the script and verify the shortcut:

```powershell
Get-ChildItem "$env:ProgramData\Microsoft\Windows\Start Menu\Programs" -Filter *.lnk -Recurse
```

### Software installs every time

Add a `detect` rule that points to a file created by the installed program:

```json
"detect": {
  "type": "file",
  "path": "C:\\Program Files\\Example\\Example.exe"
}
```

## Update or remove a package

To update a package:

1. Copy the new installer into its package folder.
2. Change its `version`, `source`, and `sha256` in `catalog.json`.
3. Restart `local-dist`.
4. Test it on one Windows computer with `setup.cmd <room-id> -Force` before deploying it to a whole room. Without `-Force`, file detection skips already-installed software regardless of its version.

To stop installing a package in a room, remove its ID from that room's `packages` list. This does not uninstall software that is already on the Windows computers.

## Developer checks

Run these after changing the Go code:

```bash
go test ./...
go vet ./...
```

The HTTP endpoints are:

- `GET /healthz` — check whether the provider is running
- `GET /api/v1/catalog` — show every package
- `GET /api/v1/rooms/{room-id}` — show one room's full package plan
- `GET /files/packages/...` — download an installer

Only files named in the loaded catalog are downloadable; temporary files, directory listings, and symbolic links are rejected. Catalog sources must be canonical relative paths under `packages/`, using `/` separators. Keep the data directory writable only by trusted administrators. The API refreshes file availability on each request; catalog and room edits still require a restart.

Package fetching uses private temporary files, verifies their checksums, and publishes them without overwriting existing installers. Concurrent fetches are supported on filesystems that support hard links. Each upstream download has a 30-minute timeout.

The provider is read-only. It cannot upload, edit, or delete packages or
client logs through the network. Windows setup logs remain on the client unless
an operator explicitly copies them elsewhere.
