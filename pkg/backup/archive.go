// SPDX-License-Identifier: AGPL-3.0-or-later
package backup

import (
	"archive/tar"
	"bytes"
	"fmt"
	"io"
	"sort"
	"time"

	"filippo.io/age"
	"github.com/ulikunitz/xz"
)

// Pack creates a tar archive from the file map, compresses with xz,
// and encrypts with an age passphrase. Returns the encrypted bytes.
func Pack(files map[string][]byte, passphrase string) ([]byte, error) {
	// 1. Create tar archive.
	var tarBuf bytes.Buffer
	tw := tar.NewWriter(&tarBuf)

	// Sort keys for deterministic output.
	names := make([]string, 0, len(files))
	for name := range files {
		names = append(names, name)
	}
	sort.Strings(names)

	for _, name := range names {
		data := files[name]
		hdr := &tar.Header{
			Name:    name,
			Size:    int64(len(data)),
			Mode:    0o644,
			ModTime: time.Now().UTC(),
		}
		if err := tw.WriteHeader(hdr); err != nil {
			return nil, fmt.Errorf("tar write header %q: %w", name, err)
		}
		if _, err := tw.Write(data); err != nil {
			return nil, fmt.Errorf("tar write body %q: %w", name, err)
		}
	}
	if err := tw.Close(); err != nil {
		return nil, fmt.Errorf("tar close: %w", err)
	}

	// 2. Compress with xz.
	var xzBuf bytes.Buffer
	xw, err := xz.NewWriter(&xzBuf)
	if err != nil {
		return nil, fmt.Errorf("xz writer: %w", err)
	}
	if _, err := xw.Write(tarBuf.Bytes()); err != nil {
		return nil, fmt.Errorf("xz write: %w", err)
	}
	if err := xw.Close(); err != nil {
		return nil, fmt.Errorf("xz close: %w", err)
	}

	// 3. Encrypt with age passphrase.
	recipient, err := age.NewScryptRecipient(passphrase)
	if err != nil {
		return nil, fmt.Errorf("age recipient: %w", err)
	}

	var ageBuf bytes.Buffer
	aw, err := age.Encrypt(&ageBuf, recipient)
	if err != nil {
		return nil, fmt.Errorf("age encrypt: %w", err)
	}
	if _, err := aw.Write(xzBuf.Bytes()); err != nil {
		return nil, fmt.Errorf("age write: %w", err)
	}
	if err := aw.Close(); err != nil {
		return nil, fmt.Errorf("age close: %w", err)
	}

	return ageBuf.Bytes(), nil
}

// Unpack decrypts with an age passphrase, decompresses xz, extracts tar,
// and returns the file map.
func Unpack(data []byte, passphrase string) (map[string][]byte, error) {
	// 1. Decrypt with age passphrase.
	identity, err := age.NewScryptIdentity(passphrase)
	if err != nil {
		return nil, fmt.Errorf("age identity: %w", err)
	}

	ar, err := age.Decrypt(bytes.NewReader(data), identity)
	if err != nil {
		return nil, fmt.Errorf("age decrypt: %w", err)
	}

	xzData, err := io.ReadAll(ar)
	if err != nil {
		return nil, fmt.Errorf("age read: %w", err)
	}

	// 2. Decompress xz.
	xr, err := xz.NewReader(bytes.NewReader(xzData))
	if err != nil {
		return nil, fmt.Errorf("xz reader: %w", err)
	}

	tarData, err := io.ReadAll(xr)
	if err != nil {
		return nil, fmt.Errorf("xz read: %w", err)
	}

	// 3. Extract tar.
	files := make(map[string][]byte)
	tr := tar.NewReader(bytes.NewReader(tarData))
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("tar next: %w", err)
		}

		body, err := io.ReadAll(tr)
		if err != nil {
			return nil, fmt.Errorf("tar read %q: %w", hdr.Name, err)
		}
		files[hdr.Name] = body
	}

	return files, nil
}
