package world

import (
	"bytes"
	"crypto/sha256"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func makeLegacyMemoLocatorTree(t *testing.T, payload []byte) (root, path, fal string) {
	t.Helper()
	root = filepath.Join(t.TempDir(), "source-root")
	player := filepath.Join(root, "player")
	fal = filepath.Join(player, "fal")
	for _, directory := range []string{root, player, fal} {
		if err := os.Mkdir(directory, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	path = filepath.Join(fal, "Bob")
	if err := os.WriteFile(path, payload, 0o600); err != nil {
		t.Fatal(err)
	}
	return root, path, fal
}

func TestLegacyMemoFileLocatorV1ReturnsOwnedSourceAndMetadata(t *testing.T) {
	payload := []byte("legacy memo source")
	root, path, _ := makeLegacyMemoLocatorTree(t, payload)

	located, err := LocateLegacyMemoFileV1(root, "Bob")
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(located.Source, payload) {
		t.Fatalf("source=%q, want %q", located.Source, payload)
	}
	if located.SHA256 != sha256.Sum256(payload) || located.Digest != located.SHA256 {
		t.Fatal("source digest does not cover returned bytes")
	}
	metadata := located.Metadata
	if metadata.Root != root || metadata.Path != path || metadata.RelativePath != filepath.Join("player", "fal", "Bob") || metadata.RecipientName != "Bob" {
		t.Fatalf("unexpected metadata=%+v", metadata)
	}
	if metadata.Size != int64(len(payload)) || metadata.Mode.Perm() != 0o600 || metadata.ModeBits != 0o600 {
		t.Fatalf("unexpected source mode/size=%+v", metadata)
	}
	if metadata.UID != uint64(os.Geteuid()) || metadata.Nlink != 1 || metadata.Inode == 0 {
		t.Fatalf("unexpected ownership metadata=%+v", metadata)
	}

	located.Source[0] = 'X'
	second, err := ReadLegacyMemoFileV1(root, "Bob")
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(second.Source, payload) {
		t.Fatal("returned source aliases filesystem or a prior call")
	}
	clone := located.Clone()
	clone.Source[1] = 'Y'
	if located.Source[1] == clone.Source[1] {
		t.Fatal("source clone aliases original")
	}
}

func TestLegacyMemoFileLocatorV1AllowsEmptySource(t *testing.T) {
	root, _, _ := makeLegacyMemoLocatorTree(t, nil)
	located, err := LocateLegacyMemoFileV1(root, "Bob")
	if err != nil {
		t.Fatal(err)
	}
	if located.Source == nil || len(located.Source) != 0 || located.Metadata.Size != 0 {
		t.Fatalf("empty source=%+v", located)
	}
}

func TestLegacyMemoFileLocatorV1ParsesOnlyAfterExplicitMapping(t *testing.T) {
	now := time.Date(2026, time.September, 10, 12, 34, 56, 0, time.UTC)
	root, _, _ := makeLegacyMemoLocatorTree(t, legacyMemoParserFixture(now, "Alice", "hello"))
	located, err := NewLegacyMemoFileLocatorV1(root)
	if err != nil {
		t.Fatal(err)
	}
	source, err := located.Locate("Bob")
	if err != nil {
		t.Fatal(err)
	}
	options := NewLegacyMemoParserOptionsV1("Bob", "bob-id", map[string]string{"Alice": "alice-id"})
	parsed, err := source.Parse(options)
	if err != nil {
		t.Fatal(err)
	}
	if parsed.RecipientID != "bob-id" || len(parsed.Memos) != 1 || parsed.Memos[0].SenderID != "alice-id" {
		t.Fatalf("parsed=%+v", parsed)
	}
	options.RecipientName = "Carol"
	if _, err := source.Parse(options); !errors.Is(err, ErrLegacyMemoIdentityConflict) {
		t.Fatalf("mismatched source name err=%v", err)
	}
}

func TestLegacyMemoFileLocatorV1RejectsSymlinkComponents(t *testing.T) {
	payload := []byte("memo")
	for _, name := range []string{"root-symlink", "player-symlink", "fal-symlink", "file-symlink"} {
		name := name
		t.Run(name, func(t *testing.T) {
			root, path, fal := makeLegacyMemoLocatorTree(t, payload)
			player := filepath.Dir(fal)
			switch name {
			case "root-symlink":
				realRoot := root + "-real"
				if err := os.Rename(root, realRoot); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(realRoot, root); err != nil {
					t.Fatal(err)
				}
			case "player-symlink":
				realPlayer := player + "-real"
				if err := os.Rename(player, realPlayer); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(realPlayer, player); err != nil {
					t.Fatal(err)
				}
			case "fal-symlink":
				realFal := fal + "-real"
				if err := os.Rename(fal, realFal); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(realFal, fal); err != nil {
					t.Fatal(err)
				}
			case "file-symlink":
				realFile := path + "-real"
				if err := os.Rename(path, realFile); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(realFile, path); err != nil {
					t.Fatal(err)
				}
			}
			if _, err := LocateLegacyMemoFileV1(root, "Bob"); !errors.Is(err, ErrLegacyMemoFileLocatorUnsafe) && !errors.Is(err, ErrLegacyMemoFileLocatorChanged) {
				t.Fatalf("symlink error=%v, want unsafe/changed", err)
			}
		})
	}
}

func TestLegacyMemoFileLocatorV1RejectsUnsafeModesLinksAndSize(t *testing.T) {
	cases := []struct {
		name   string
		mutate func(root, path, fal string) error
		want   error
	}{
		{name: "root-mode", mutate: func(root, path, fal string) error { return os.Chmod(root, 0o750) }, want: ErrLegacyMemoFileLocatorUnsafe},
		{name: "player-mode", mutate: func(root, path, fal string) error { return os.Chmod(filepath.Dir(fal), 0o750) }, want: ErrLegacyMemoFileLocatorUnsafe},
		{name: "fal-mode", mutate: func(root, path, fal string) error { return os.Chmod(fal, 0o750) }, want: ErrLegacyMemoFileLocatorUnsafe},
		{name: "file-mode", mutate: func(root, path, fal string) error { return os.Chmod(path, 0o640) }, want: ErrLegacyMemoFileLocatorUnsafe},
		{name: "hard-link", mutate: func(root, path, fal string) error { return os.Link(path, path+".hard") }, want: ErrLegacyMemoFileLocatorUnsafe},
		{name: "size-limit", mutate: func(root, path, fal string) error { return os.Truncate(path, int64(LegacyMemoFileLocatorV1MaxBytes)+1) }, want: ErrLegacyMemoFileLocatorSizeLimit},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			root, path, fal := makeLegacyMemoLocatorTree(t, []byte("memo"))
			if err := tc.mutate(root, path, fal); err != nil {
				t.Fatal(err)
			}
			if _, err := LocateLegacyMemoFileV1(root, "Bob"); !errors.Is(err, tc.want) {
				t.Fatalf("error=%v, want errors.Is(...,%v)", err, tc.want)
			}
		})
	}
}

func TestLegacyMemoFileLocatorV1ClassifiesMissingAndInvalidInputs(t *testing.T) {
	root, path, fal := makeLegacyMemoLocatorTree(t, []byte("memo"))
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if _, err := LocateLegacyMemoFileV1(root, "Bob"); !errors.Is(err, ErrLegacyMemoFileLocatorNotFound) || !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("missing file error=%v", err)
	}
	if _, err := LocateLegacyMemoFileV1(root, "Bob"); !errors.Is(err, ErrLegacyMemoFileLocatorNotFound) {
		t.Fatalf("repeat missing file error=%v", err)
	}

	_ = fal
	names := []string{"", ".", "..", "alice", "../Bob", "Bob/other", `Bob\\other`, "Bob:other", "bad\x01name", string([]byte{0xc3, 0x28}), strings.Repeat("x", 13)}
	for _, name := range names {
		t.Run(name, func(t *testing.T) {
			if _, err := LocateLegacyMemoFileV1(root, name); !errors.Is(err, ErrLegacyMemoFileLocatorInvalidName) &&
				!errors.Is(err, ErrLegacyMemoFileLocatorPathTraversal) && !errors.Is(err, ErrLegacyMemoFileLocatorNonCanonicalName) {
				t.Fatalf("name=%q error=%v", name, err)
			}
		})
	}
	for _, invalidRoot := range []string{"relative/root", "/", root + string(filepath.Separator) + ".." + string(filepath.Separator) + filepath.Base(root)} {
		if _, err := LocateLegacyMemoFileV1(invalidRoot, "Bob"); !errors.Is(err, ErrLegacyMemoFileLocatorInvalidRoot) && !errors.Is(err, ErrLegacyMemoFileLocatorPathTraversal) {
			t.Fatalf("root=%q error=%v", invalidRoot, err)
		}
	}
}
