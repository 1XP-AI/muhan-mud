package world

import (
	"errors"
	"io"
	"io/fs"
	"os"
	"testing"
)

func TestLegacyRoomAdmissionManifestReportsReviewedCorpus(t *testing.T) {
	catalog, err := LoadLegacyRoomCatalog(os.DirFS("../../../rooms"), LegacyRoomCompatibilityPolicy)
	if err != nil {
		t.Fatal(err)
	}
	manifest := catalog.AdmissionManifest()
	if got, want := manifest.TotalFiles(), 3216; got != want {
		t.Fatalf("manifest total files: got %d, want %d", got, want)
	}
	if got, want := manifest.CanonicalPaths(), 2341; got != want {
		t.Fatalf("manifest canonical paths: got %d, want %d", got, want)
	}
	if got, want := manifest.AdmittedCanonicalPaths(), 2341; got != want {
		t.Fatalf("manifest admitted canonical paths: got %d, want %d", got, want)
	}
	if got, want := manifest.NoncanonicalArtifacts(), 875; got != want {
		t.Fatalf("manifest noncanonical artifacts: got %d, want %d", got, want)
	}
	if got, want := manifest.BodyExceptions(), 63; got != want {
		t.Fatalf("manifest body exceptions: got %d, want %d", got, want)
	}
	issues := manifest.IssueCounts()
	for kind, want := range map[string]int{
		"invalid-euc-kr":          80,
		"missing-text-terminator": 13,
		"trailing-data":           7,
	} {
		if got := issues[kind]; got != want {
			t.Fatalf("manifest %s issues: got %d, want %d", kind, got, want)
		}
	}
	if got := len(manifest.Entries()); got != 3216 {
		t.Fatalf("manifest entries: got %d, want 3216", got)
	}
	if err := catalog.VerifyReviewedAdmissionManifest(); err != nil {
		t.Fatal(err)
	}
}

func TestLegacyRoomAdmissionManifestPreservesArtifactEvidence(t *testing.T) {
	catalog, err := LoadLegacyRoomCatalog(os.DirFS("../../../rooms"), LegacyRoomCompatibilityPolicy)
	if err != nil {
		t.Fatal(err)
	}
	ignored := catalog.Ignored()
	if len(ignored) != 875 {
		t.Fatalf("ignored artifacts: got %d, want 875", len(ignored))
	}
	for _, artifact := range ignored {
		if artifact.Kind != "noncanonical-path" {
			t.Fatalf("artifact %q has unexpected classification %q", artifact.Path, artifact.Kind)
		}
		if artifact.Size <= 0 || artifact.SHA256 == [32]byte{} {
			t.Fatalf("artifact %q lost source hash/size evidence: %+v", artifact.Path, artifact)
		}
	}
	entries := catalog.AdmissionManifest().Entries()
	var starting, historical *LegacyRoomManifestEntry
	for i := range entries {
		switch entries[i].Path {
		case "r00/r00001":
			starting = &entries[i]
		case "r01/r00100":
			historical = &entries[i]
		}
	}
	if starting == nil || starting.Class != LegacyRoomCanonical || !starting.Admitted {
		t.Fatalf("canonical room was not proven admitted: %+v", starting)
	}
	if historical == nil || historical.Class != LegacyRoomNoncanonicalArtifact || historical.Admitted {
		t.Fatalf("historical artifact was not quarantined: %+v", historical)
	}
	for _, entry := range entries {
		if entry.Class != LegacyRoomNoncanonicalArtifact {
			continue
		}
		if entry.SHA256 == [32]byte{} || entry.Size <= 0 {
			t.Fatalf("noncanonical manifest entry lost evidence: %+v", entry)
		}
		if entry.Path == "" {
			t.Fatal("noncanonical manifest entry has empty path")
		}
	}
}

func TestLegacyRoomAdmissionManifestClassifiesBodyExceptions(t *testing.T) {
	catalog, err := LoadLegacyRoomCatalog(os.DirFS("../../../rooms"), LegacyRoomCompatibilityPolicy)
	if err != nil {
		t.Fatal(err)
	}
	bodyExceptions := 0
	for _, entry := range catalog.AdmissionManifest().Entries() {
		if !entry.BodyException {
			continue
		}
		bodyExceptions++
		if entry.SHA256 == [32]byte{} || entry.Consumed <= 0 || len(entry.Issues) == 0 {
			t.Fatalf("body exception lost source/issue evidence: %+v", entry)
		}
	}
	if bodyExceptions != 63 {
		t.Fatalf("body exception count: got %d, want 63", bodyExceptions)
	}
}

func TestLegacyRoomAdmissionManifestFailsClosedOnDrift(t *testing.T) {
	catalog, err := LoadLegacyRoomCatalog(os.DirFS("../../../rooms"), LegacyRoomCompatibilityPolicy)
	if err != nil {
		t.Fatal(err)
	}
	base := catalog.AdmissionManifest()
	entries := base.Entries()
	entries[0].SHA256[0] ^= 0xff
	if err := base.Verify(entries); err == nil {
		t.Fatal("accepted a manifest entry hash drift")
	}

	fsys := &legacyManifestDriftFS{base: os.DirFS("../../../rooms"), path: "r00/r00001"}
	if _, err := LoadReviewedLegacyRoomCatalog(fsys, LegacyRoomCompatibilityPolicy); err == nil {
		t.Fatal("reviewed loader accepted source byte drift")
	} else {
		var driftErr LegacyRoomManifestDriftError
		if !errors.As(err, &driftErr) {
			t.Fatalf("wrong drift error: %v", err)
		}
	}
}

// legacyManifestDriftFS overlays one source file without changing directory
// enumeration. It lets the test exercise the same read boundary used by the
// reviewed loader without copying the 3,216-file corpus into a map fixture.
type legacyManifestDriftFS struct {
	base fs.FS
	path string
}

func (f *legacyManifestDriftFS) Open(name string) (fs.File, error) {
	file, err := f.base.Open(name)
	if err != nil {
		return nil, err
	}
	if name != f.path {
		return file, nil
	}
	info, err := file.Stat()
	if err != nil {
		_ = file.Close()
		return nil, err
	}
	raw, err := fs.ReadFile(f.base, name)
	_ = file.Close()
	if err != nil {
		return nil, err
	}
	raw = append([]byte(nil), raw...)
	raw[0] ^= 0x01
	return &legacyManifestDriftFile{FileInfo: info, data: raw}, nil
}

type legacyManifestDriftFile struct {
	fs.FileInfo
	data []byte
	off  int
}

func (f *legacyManifestDriftFile) Read(p []byte) (int, error) {
	if f.off >= len(f.data) {
		return 0, io.EOF
	}
	n := copy(p, f.data[f.off:])
	f.off += n
	return n, nil
}

func (f *legacyManifestDriftFile) Stat() (fs.FileInfo, error) { return f.FileInfo, nil }

func (f *legacyManifestDriftFile) Close() error { return nil }
