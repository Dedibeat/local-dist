# Project guidance

## Purpose and layout

This project distributes approved Windows installers from a Linux server to
computers on a trusted local network. Keep the provider small and read-only,
and keep the client compatible with built-in Windows PowerShell 5.1.

- `cmd/local-dist/`: HTTP server entry point and runtime configuration.
- `cmd/fetch-packages/`: command for fetching and verifying catalog installers.
- `internal/provider/`: catalog validation, room plans, file serving, downloads,
  and Go tests.
- `data/catalog.json`: approved package metadata, sources, and SHA-256 hashes.
- `data/rooms.json`: room assignments and outstanding requirements.
- `data/packages/`: local installer payloads, ignored by Git except `.gitkeep`.
- `scripts/`: Windows installation client and Linux package-copy helper.
- `deployments/`, `Dockerfile`, and `compose.yaml`: deployment examples.
- `reference/`: original room requirements and planning notes.
- `README.md`: operator instructions; update it when behavior or commands change.

## Implementation conventions

- Use Go 1.23-compatible code and prefer the standard library. Keep reusable
  behavior in `internal/provider` and command entry points thin.
- Format changed Go files with `gofmt`. Add regression tests for bug fixes that
  affect validation, downloads, or HTTP behavior.
- Preserve existing user changes and approved package data. Do not infer missing
  room requirements or mark a room complete while requirements remain pending.
- Keep catalog and room schema version 1 unless a migration is explicitly part
  of the task. Package references must resolve and IDs must remain unique.
- Do not commit installers, build outputs, credentials, or temporary downloads.

## Safety and behavior to preserve

- Serve only files named in the loaded catalog. Reject directory listings,
  traversal, symlinks, and special files; never expose configuration or partial
  downloads through the file endpoint.
- Treat package sources as canonical relative paths below `packages/`, with `/`
  separators. Escape generated download URLs and retain HEAD and range support.
- Refresh file availability for API requests without mutating shared catalog
  state. Catalog and room edits require a provider restart.
- Verify SHA-256 before publishing or executing an installer. Never replace a
  checksum simply to make a failed verification pass; establish provenance first.
- Fetch upstream packages over HTTPS, including redirects. Preserve cancellation,
  bounded download time, unique temporary files, cleanup, and atomic publication
  that does not overwrite an existing installer. Publication requires hard links.
- Package directories must be writable only by trusted administrators. Default
  HTTP does not authenticate the server; checksums from the same HTTP catalog
  do not protect against a modified catalog.
- The Windows client requires elevation. File detection checks existence, not
  version; `-Force` reinstalls the selected room's packages. Maintain PATH repair
  for detected packages and exit codes `0` (success), `3010` (restart required),
  and `1` (failure).
- Preserve support for MSI, EXE, ZIP, and portable packages. Installer arguments
  are publisher-specific; avoid changing silent-install options without evidence.

## Verification

Run these for Go changes:

```bash
go test -race ./...
go vet ./...
go build ./...
```

For changes to the package-copy helper, run `bash -n scripts/add-package.sh` and
exercise copying, readable permissions, overwrite refusal, and temporary-file
cleanup in a temporary directory.

Validate PowerShell changes on Windows PowerShell 5.1 when available. Use a test
machine for actual installation checks. If Windows is unavailable, report that
limitation; Linux Go checks do not verify Windows installation behavior.

`go run ./cmd/fetch-packages -data ./data` verifies existing installers and
downloads missing ones. It is not an offline test: use it when restoring or
checking package payloads is in scope. Use local test servers for automated
download tests rather than depending on live upstream services.
