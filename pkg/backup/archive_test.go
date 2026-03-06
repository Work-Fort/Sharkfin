// SPDX-License-Identifier: AGPL-3.0-or-later
package backup_test

import (
	"testing"

	"github.com/Work-Fort/sharkfin/pkg/backup"
)

func TestPackUnpackRoundtrip(t *testing.T) {
	files := map[string][]byte{
		"config.yaml": []byte("server:\n  port: 8080\n"),
		"data.json":   []byte(`{"users":["alice","bob"]}`),
	}

	packed, err := backup.Pack(files, "test-passphrase")
	if err != nil {
		t.Fatalf("Pack: %v", err)
	}

	if len(packed) == 0 {
		t.Fatal("Pack returned empty data")
	}

	unpacked, err := backup.Unpack(packed, "test-passphrase")
	if err != nil {
		t.Fatalf("Unpack: %v", err)
	}

	if len(unpacked) != len(files) {
		t.Fatalf("unpacked %d files, want %d", len(unpacked), len(files))
	}

	for name, expected := range files {
		got, ok := unpacked[name]
		if !ok {
			t.Errorf("file %q not found in unpacked output", name)
			continue
		}
		if string(got) != string(expected) {
			t.Errorf("file %q: got %q, want %q", name, got, expected)
		}
	}
}

func TestUnpackWrongPassphrase(t *testing.T) {
	files := map[string][]byte{
		"secret.txt": []byte("top secret"),
	}

	packed, err := backup.Pack(files, "correct")
	if err != nil {
		t.Fatalf("Pack: %v", err)
	}

	_, err = backup.Unpack(packed, "wrong")
	if err == nil {
		t.Fatal("Unpack with wrong passphrase should return error")
	}
}
