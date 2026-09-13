package provider

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
)

func FetchPackages(ctx context.Context, store *Store, client *http.Client, output io.Writer) error {
	// Preserve the caller's transport and timeout, while enforcing HTTPS on
	// redirects as well as the original URL. Do not mutate a shared client.
	downloadClient := *client
	downloadClient.CheckRedirect = func(request *http.Request, via []*http.Request) error {
		if request.URL.Scheme != "https" {
			return fmt.Errorf("upstream redirect must use HTTPS")
		}
		if client.CheckRedirect != nil {
			return client.CheckRedirect(request, via)
		}
		if len(via) >= 10 {
			return fmt.Errorf("stopped after 10 redirects")
		}
		return nil
	}
	for _, pkg := range store.Catalog.Packages {
		if err := ctx.Err(); err != nil {
			return err
		}
		target, err := store.packagePath(pkg.Source)
		if err != nil {
			return fmt.Errorf("check %s: %w", pkg.ID, err)
		}
		matches, err := fileMatchesSHA256(target, pkg.SHA256)
		if err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("check %s: %w", pkg.ID, err)
		}
		if matches {
			fmt.Fprintf(output, "Ready: %s %s\n", pkg.Name, pkg.Version)
			continue
		}
		if err == nil {
			return fmt.Errorf("%s exists but its SHA-256 does not match the catalog", target)
		}
		if !strings.HasPrefix(pkg.UpstreamURL, "https://") {
			return fmt.Errorf("package %s is missing and has no HTTPS upstreamUrl", pkg.ID)
		}

		fmt.Fprintf(output, "Downloading: %s %s\n", pkg.Name, pkg.Version)
		if err := downloadPackage(ctx, &downloadClient, pkg, target); err != nil {
			return err
		}
	}
	return nil
}

func downloadPackage(ctx context.Context, client *http.Client, pkg Package, target string) error {
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		return fmt.Errorf("create package directory for %s: %w", pkg.ID, err)
	}

	file, err := os.CreateTemp(filepath.Dir(target), ".download-*.part")
	if err != nil {
		return fmt.Errorf("create temporary file for %s: %w", pkg.ID, err)
	}
	temporary := file.Name()
	defer func() {
		_ = file.Close()
		_ = os.Remove(temporary)
	}()

	request, err := http.NewRequestWithContext(ctx, http.MethodGet, pkg.UpstreamURL, nil)
	if err != nil {
		return fmt.Errorf("create request for %s: %w", pkg.ID, err)
	}
	response, err := client.Do(request)
	if err != nil {
		return fmt.Errorf("download %s: %w", pkg.ID, err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return fmt.Errorf("download %s: server returned %s", pkg.ID, response.Status)
	}

	hash := sha256.New()
	if _, err := io.Copy(io.MultiWriter(file, hash), response.Body); err != nil {
		return fmt.Errorf("save %s: %w", pkg.ID, err)
	}
	actual := hex.EncodeToString(hash.Sum(nil))
	if !strings.EqualFold(actual, pkg.SHA256) {
		return fmt.Errorf("download %s: SHA-256 mismatch: expected %s, received %s", pkg.ID, pkg.SHA256, actual)
	}
	if err := file.Chmod(0o644); err != nil {
		return fmt.Errorf("set permissions for %s: %w", pkg.ID, err)
	}
	if err := file.Close(); err != nil {
		return fmt.Errorf("close %s: %w", pkg.ID, err)
	}
	// Publish atomically without overwriting a file created by another fetch.
	if err := os.Link(temporary, target); err != nil {
		if os.IsExist(err) {
			if matches, checkErr := fileMatchesSHA256(target, pkg.SHA256); checkErr == nil && matches {
				return nil
			}
		}
		return fmt.Errorf("finish %s: %w", pkg.ID, err)
	}
	return nil
}

func fileMatchesSHA256(path, expected string) (bool, error) {
	file, err := os.Open(path)
	if err != nil {
		return false, err
	}
	defer file.Close()

	hash := sha256.New()
	if _, err := io.Copy(hash, file); err != nil {
		return false, err
	}
	return strings.EqualFold(hex.EncodeToString(hash.Sum(nil)), expected), nil
}
