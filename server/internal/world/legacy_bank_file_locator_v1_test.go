package world

import (
	"bytes"
	"crypto/sha256"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func makeLegacyBankLocatorTree(t *testing.T, payload []byte) (string, string, string) {
	t.Helper()
	root := t.TempDir()
	player := filepath.Join(root, "player")
	bank := filepath.Join(player, "bank")
	if err := os.Mkdir(player, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(bank, 0o700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(bank, "Alice")
	if err := os.WriteFile(path, payload, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(root, 0o700); err != nil {
		t.Fatal(err)
	}
	return root, path, bank
}

func TestLegacyBankFileLocatorV1ReturnsOwnedRawSourceAndMetadata(t *testing.T) {
	payload := []byte("native bank bytes are decoded by a different migration stage")
	root, path, _ := makeLegacyBankLocatorTree(t, payload)

	located, err := LocateLegacyBankSnapshotRawV1(root, "Alice")
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(located.Source, payload) {
		t.Fatalf("source=%q, want %q", located.Source, payload)
	}
	if located.SHA256 != sha256.Sum256(payload) || located.Digest != located.SHA256 {
		t.Fatal("source digest does not cover the returned bytes")
	}
	metadata := located.Metadata
	if metadata.Root != root || metadata.Path != path || metadata.RelativePath != filepath.Join("player", "bank", "Alice") || metadata.PlayerName != "Alice" {
		t.Fatalf("unexpected source path metadata: %+v", metadata)
	}
	if metadata.Size != int64(len(payload)) || metadata.Mode.Perm() != 0o600 || metadata.ModeBits != 0o600 {
		t.Fatalf("unexpected file metadata: %+v", metadata)
	}
	if metadata.UID != uint64(os.Geteuid()) || metadata.Nlink != 1 || metadata.Inode == 0 {
		t.Fatalf("unexpected ownership metadata: %+v", metadata)
	}

	located.Source[0] = 'X'
	second, err := LocateLegacyBankSnapshotRawV1(root, "Alice")
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(second.Source, payload) {
		t.Fatal("returned source aliases a previous call or the filesystem buffer")
	}
	clone := located.Clone()
	clone.Source[1] = 'Y'
	if located.Source[1] == clone.Source[1] {
		t.Fatal("source clone aliases the original result")
	}
}

func TestLegacyBankFileLocatorV1AllowsEmptySourceLikeCLocator(t *testing.T) {
	root, _, _ := makeLegacyBankLocatorTree(t, nil)
	located, err := LocateLegacyBankSnapshotRawV1(root, "Alice")
	if err != nil {
		t.Fatal(err)
	}
	if located.Source == nil || len(located.Source) != 0 || located.Metadata.Size != 0 {
		t.Fatalf("empty source result=%+v, want an owned zero-byte source", located)
	}
}

func TestLegacyBankFileLocatorV1UsesDescriptorAnchoredPrivateTree(t *testing.T) {
	payload := []byte("bank")
	for _, caseName := range []string{"root-symlink", "player-symlink", "bank-symlink", "file-symlink"} {
		caseName := caseName
		t.Run(caseName, func(t *testing.T) {
			// Rebuild each case so the previous rename/symlink cannot affect the
			// descriptor walk used by the next one.
			caseRoot, casePath, caseBank := makeLegacyBankLocatorTree(t, payload)
			casePlayer := filepath.Dir(caseBank)
			switch caseName {
			case "root-symlink":
				realRoot := t.TempDir()
				if err := os.Rename(filepath.Join(caseRoot, "player"), filepath.Join(realRoot, "player")); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(realRoot, caseRoot+"-link"); err != nil {
					t.Fatal(err)
				}
				caseRoot += "-link"
			case "player-symlink":
				if err := os.Rename(casePlayer, casePlayer+"-real"); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(casePlayer+"-real", casePlayer); err != nil {
					t.Fatal(err)
				}
			case "bank-symlink":
				if err := os.Rename(caseBank, caseBank+"-real"); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(caseBank+"-real", caseBank); err != nil {
					t.Fatal(err)
				}
			case "file-symlink":
				if err := os.Rename(casePath, casePath+"-real"); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(casePath+"-real", casePath); err != nil {
					t.Fatal(err)
				}
			}
			if _, err := LocateLegacyBankSnapshotRawV1(caseRoot, "Alice"); !errors.Is(err, ErrLegacyBankFileLocatorUnsafe) && !errors.Is(err, ErrLegacyBankFileLocatorChanged) {
				t.Fatalf("symlink escape error=%v, want unsafe/changed", err)
			}
		})
	}
}

func TestLegacyBankFileLocatorV1RejectsUnsafeModesLinksAndBounds(t *testing.T) {
	payload := []byte("bank")
	cases := []struct {
		name   string
		mutate func(root, path, bank string) error
		want   error
	}{
		{name: "player-mode", mutate: func(root, path, bank string) error {
			return os.Chmod(filepath.Dir(bank), 0o750)
		}, want: ErrLegacyBankFileLocatorUnsafe},
		{name: "root-mode", mutate: func(root, path, bank string) error {
			return os.Chmod(root, 0o750)
		}, want: ErrLegacyBankFileLocatorUnsafe},
		{name: "bank-mode", mutate: func(root, path, bank string) error {
			return os.Chmod(bank, 0o750)
		}, want: ErrLegacyBankFileLocatorUnsafe},
		{name: "file-mode", mutate: func(root, path, bank string) error {
			return os.Chmod(path, 0o640)
		}, want: ErrLegacyBankFileLocatorUnsafe},
		{name: "hard-link", mutate: func(root, path, bank string) error {
			return os.Link(path, path+".hard")
		}, want: ErrLegacyBankFileLocatorUnsafe},
		{name: "size-limit", mutate: func(root, path, bank string) error {
			return os.Truncate(path, int64(LegacyBankFileLocatorV1MaxBytes)+1)
		}, want: ErrLegacyBankFileLocatorSizeLimit},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			root, path, bank := makeLegacyBankLocatorTree(t, payload)
			if err := tc.mutate(root, path, bank); err != nil {
				t.Fatal(err)
			}
			if _, err := LocateLegacyBankSnapshotRawV1(root, "Alice"); !errors.Is(err, tc.want) {
				t.Fatalf("error=%v, want errors.Is(..., %v)", err, tc.want)
			}
		})
	}
}

func TestLegacyBankFileLocatorV1DistinguishesMissingSource(t *testing.T) {
	root, _, bank := makeLegacyBankLocatorTree(t, []byte("bank"))
	if err := os.Remove(filepath.Join(bank, "Alice")); err != nil {
		t.Fatal(err)
	}
	_, err := LocateLegacyBankSnapshotRawV1(root, "Alice")
	if !errors.Is(err, ErrLegacyBankFileLocatorNotFound) || !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("missing source error=%v, want locator/not-exist classification", err)
	}
}

func TestLegacyBankFileLocatorV1RejectsInvalidNamesAndRootsBeforeOpen(t *testing.T) {
	root, _, _ := makeLegacyBankLocatorTree(t, []byte("bank"))
	names := []string{"", ".", "..", "../Alice", "Alice/other", `Alice\\other`, "Alice:other", "bad\x01name", string([]byte{0xc3, 0x28}), strings.Repeat("x", 13)}
	for _, name := range names {
		name := name
		t.Run(name, func(t *testing.T) {
			_, err := LocateLegacyBankSnapshotRawV1(root, name)
			if !errors.Is(err, ErrLegacyBankFileLocatorInvalidName) && !errors.Is(err, ErrLegacyBankFileLocatorPathTraversal) {
				t.Fatalf("name=%q error=%v, want invalid name/path traversal", name, err)
			}
		})
	}
	if _, err := LocateLegacyBankSnapshotRawV1("relative/root", "Alice"); !errors.Is(err, ErrLegacyBankFileLocatorInvalidRoot) {
		t.Fatalf("relative root error=%v", err)
	}
	traversalRoot := root + string(filepath.Separator) + ".." + string(filepath.Separator) + filepath.Base(root)
	if _, err := LocateLegacyBankSnapshotRawV1(traversalRoot, "Alice"); !errors.Is(err, ErrLegacyBankFileLocatorPathTraversal) {
		t.Fatalf("traversal root error=%v", err)
	}
	if _, err := NewLegacyBankFileLocatorV1(root); err != nil {
		t.Fatal(err)
	}
}
