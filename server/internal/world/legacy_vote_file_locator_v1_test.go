package world

import (
	"bytes"
	"crypto/sha256"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func makeLegacyVoteLocatorTree(t *testing.T, files map[string][]byte) (root, voteDir string) {
	t.Helper()
	root = filepath.Join(t.TempDir(), "source-root")
	player := filepath.Join(root, "player")
	voteDir = filepath.Join(player, "vote")
	for _, directory := range []string{root, player, voteDir} {
		if err := os.Mkdir(directory, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	for name, payload := range files {
		if err := os.WriteFile(filepath.Join(voteDir, name+"_v"), payload, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	return root, voteDir
}

func TestLegacyVoteFileLocatorV1ReturnsOwnedSourceAndMetadata(t *testing.T) {
	payload := []byte("AB")
	root, _ := makeLegacyVoteLocatorTree(t, map[string][]byte{"Bob": payload})

	located, err := LocateLegacyVoteFileV1(root, "Bob")
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(located.Source, payload) || located.SHA256 != sha256.Sum256(payload) || located.Digest != located.SHA256 {
		t.Fatalf("source=%q digest=%x", located.Source, located.SHA256)
	}
	metadata := located.Metadata
	wantPath := filepath.Join(root, "player", "vote", "Bob_v")
	if metadata.Root != root || metadata.Path != wantPath || metadata.RelativePath != filepath.Join("player", "vote", "Bob_v") || metadata.PlayerName != "Bob" {
		t.Fatalf("metadata=%+v", metadata)
	}
	if metadata.Size != int64(len(payload)) || metadata.Mode.Perm() != 0o600 || metadata.UID != uint64(os.Geteuid()) || metadata.Nlink != 1 || metadata.Inode == 0 {
		t.Fatalf("metadata=%+v", metadata)
	}

	located.Source[0] = 'X'
	second, err := LocateLegacyVoteFileV1(root, "Bob")
	if err != nil || !bytes.Equal(second.Source, payload) {
		t.Fatalf("second=%q err=%v", second.Source, err)
	}
	clone := located.Clone()
	clone.Source[1] = 'Y'
	if located.Source[1] == clone.Source[1] {
		t.Fatal("source clone aliases original")
	}
}

func TestLegacyVoteFileLocatorV1EnumeratesLexicallyAndAllowsEmptySource(t *testing.T) {
	root, _ := makeLegacyVoteLocatorTree(t, map[string][]byte{"Zoo": []byte("B"), "Bob": []byte("A")})
	all, err := LocateLegacyVoteRawFilesV1(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(all) != 2 || all[0].Metadata.PlayerName != "Bob" || all[1].Metadata.PlayerName != "Zoo" {
		t.Fatalf("all=%+v", all)
	}
	emptyRoot, _ := makeLegacyVoteLocatorTree(t, nil)
	empty, err := LocateLegacyVoteRawFilesV1(emptyRoot)
	if err != nil || empty == nil || len(empty) != 0 {
		t.Fatalf("empty=%+v err=%v", empty, err)
	}
}

func TestLegacyVoteFileLocatorV1RejectsSymlinksModesUnexpectedAndOversize(t *testing.T) {
	t.Run("symlink-file", func(t *testing.T) {
		root, voteDir := makeLegacyVoteLocatorTree(t, map[string][]byte{"Bob": []byte("A")})
		path := filepath.Join(voteDir, "Bob_v")
		if err := os.Rename(path, path+"-real"); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(path+"-real", path); err != nil {
			t.Fatal(err)
		}
		if _, err := LocateLegacyVoteFileV1(root, "Bob"); !errors.Is(err, ErrLegacyVoteFileLocatorUnsafe) && !errors.Is(err, ErrLegacyVoteFileLocatorChanged) {
			t.Fatalf("err=%v", err)
		}
	})
	t.Run("unsafe-mode", func(t *testing.T) {
		root, voteDir := makeLegacyVoteLocatorTree(t, map[string][]byte{"Bob": []byte("A")})
		if err := os.Chmod(filepath.Join(voteDir, "Bob_v"), 0o640); err != nil {
			t.Fatal(err)
		}
		if _, err := LocateLegacyVoteFileV1(root, "Bob"); !errors.Is(err, ErrLegacyVoteFileLocatorUnsafe) {
			t.Fatalf("err=%v", err)
		}
	})
	t.Run("unexpected-entry", func(t *testing.T) {
		root, voteDir := makeLegacyVoteLocatorTree(t, map[string][]byte{"Bob": []byte("A")})
		if err := os.WriteFile(filepath.Join(voteDir, ".keep"), []byte("x"), 0o600); err != nil {
			t.Fatal(err)
		}
		if _, err := LocateLegacyVoteRawFilesV1(root); !errors.Is(err, ErrLegacyVoteFileLocatorUnexpected) {
			t.Fatalf("err=%v", err)
		}
	})
	t.Run("oversize", func(t *testing.T) {
		root, voteDir := makeLegacyVoteLocatorTree(t, map[string][]byte{"Bob": []byte("A")})
		if err := os.Truncate(filepath.Join(voteDir, "Bob_v"), int64(LegacyVoteFileLocatorV1MaxBytes)+1); err != nil {
			t.Fatal(err)
		}
		if _, err := LocateLegacyVoteFileV1(root, "Bob"); !errors.Is(err, ErrLegacyVoteFileLocatorSizeLimit) {
			t.Fatalf("err=%v", err)
		}
	})
}

func TestLegacyVoteFileLocatorV1RejectsInvalidNamesAndRoots(t *testing.T) {
	root, _ := makeLegacyVoteLocatorTree(t, map[string][]byte{"Bob": []byte("A")})
	for _, name := range []string{"", ".", "..", "bob", "../Bob", "Bob/other", `Bob\\other`, "Bob:other", "bad\x01name", string([]byte{0xc3, 0x28})} {
		t.Run(name, func(t *testing.T) {
			if _, err := LocateLegacyVoteFileV1(root, name); !errors.Is(err, ErrLegacyVoteFileLocatorInvalidName) && !errors.Is(err, ErrLegacyVoteFileLocatorPathTraversal) {
				t.Fatalf("name=%q err=%v", name, err)
			}
		})
	}
	for _, invalidRoot := range []string{"relative/root", "/", root + string(filepath.Separator) + ".." + string(filepath.Separator) + filepath.Base(root)} {
		if _, err := LocateLegacyVoteFileV1(invalidRoot, "Bob"); !errors.Is(err, ErrLegacyVoteFileLocatorInvalidRoot) && !errors.Is(err, ErrLegacyVoteFileLocatorPathTraversal) {
			t.Fatalf("root=%q err=%v", invalidRoot, err)
		}
	}
}
