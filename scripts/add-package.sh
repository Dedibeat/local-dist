#!/usr/bin/env bash
set -euo pipefail

if [[ $# -ne 2 ]]; then
  echo "Usage: $0 <installer-file> <package-directory>" >&2
  echo "Example: $0 ~/Downloads/7z2409-x64.exe data/packages/7zip" >&2
  exit 2
fi

installer_file=$1
package_directory=$2

if [[ ! -f "$installer_file" ]]; then
  echo "Installer does not exist: $installer_file" >&2
  exit 1
fi

umask 022
mkdir -p -- "$package_directory"
installed_path="$package_directory/$(basename -- "$installer_file")"
temporary_file=$(mktemp -- "$package_directory/.add-package.XXXXXX")
trap 'rm -f -- "$temporary_file"' EXIT
cp -- "$installer_file" "$temporary_file"
chmod 0644 -- "$temporary_file"
# Publish a complete, readable installer without replacing an approved file.
if ! ln -T -- "$temporary_file" "$installed_path"; then
  echo "Could not add installer; destination must not already exist: $installed_path" >&2
  exit 1
fi

echo "Copied to: $installed_path"
echo "SHA256: $(sha256sum "$installed_path" | cut -d ' ' -f 1)"
