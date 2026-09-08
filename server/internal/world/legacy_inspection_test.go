package world

import (
	"bytes"
	"crypto/sha256"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestInspectionCorpus(t *testing.T) {
	paths, err := filepath.Glob("../../../rooms/r*/r?????")
	if err != nil || len(paths) == 0 {
		t.Fatal("missing corpus", err)
	}
	kinds := map[string]int{}
	issueRooms, failed := 0, 0
	for _, path := range paths {
		raw, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		report, err := InspectLegacyRoom(raw)
		if err != nil {
			failed++
			t.Logf("%s: %v", path, err)
			continue
		}
		if !bytes.Equal(report.Source, raw) || report.SHA256 != sha256.Sum256(raw) {
			t.Fatalf("lost source: %s", path)
		}
		strict, strictErr := DecodeLegacyRoom(raw)
		if len(report.Issues) > 0 {
			issueRooms++
			if strictErr == nil {
				t.Fatalf("strict silently accepted %s", path)
			}
			for _, issue := range report.Issues {
				kinds[issue.Kind]++
			}
		} else if strictErr != nil || !reflect.DeepEqual(strict, report.Room) {
			t.Fatalf("inspect/strict mismatch: %s", path)
		}
	}
	t.Logf("rooms=%d issueRooms=%d issues=%v structuralFailures=%d", len(paths), issueRooms, kinds, failed)
	if failed != 0 {
		t.Fatal("structural inspection incomplete")
	}
}

func TestInspectionPreservesNoncanonicalSource(t *testing.T) {
	for _, name := range []string{"r00100", "r00173"} {
		raw, err := os.ReadFile("../../../rooms/r00/" + name)
		if err != nil {
			t.Fatal(err)
		}
		before := append([]byte(nil), raw...)
		report, err := InspectLegacyRoom(raw)
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(report.Source, before) || report.SHA256 != sha256.Sum256(before) || len(report.Issues) == 0 {
			t.Fatal("lost original evidence")
		}
		if _, err := DecodeLegacyRoom(raw); err == nil {
			t.Fatal("strict decoder accepted noncanonical source")
		}
		for _, issue := range report.Issues {
			if issue.Offset < 0 || issue.Offset >= len(raw) {
				t.Fatal("invalid issue location")
			}
		}
		raw[0] ^= 0xff
		if !bytes.Equal(report.Source, before) {
			t.Fatal("source evidence aliases caller memory")
		}
	}
}

func TestInspectionDoesNotRepairBrokenStructure(t *testing.T) {
	raw, err := os.ReadFile("../../../rooms/r00/r00001")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := InspectLegacyRoom(raw[:len(raw)-1]); err == nil {
		t.Fatal("accepted truncation")
	}
	for i := 528; i < 532; i++ {
		raw[i] = 0xff
	}
	if _, err := InspectLegacyRoom(raw); err == nil {
		t.Fatal("accepted negative count")
	}
}

func TestInspectionBoundsUnterminatedFixedText(t *testing.T) {
	raw := make([]byte, 484)
	raw = append(raw, 0, 0, 0, 0, 1, 0, 0, 0)
	o := make([]byte, 352)
	for i := 0; i < 80; i++ {
		o[i] = 'A'
	}
	copy(o[80:], "neighbor")
	raw = append(raw, o...)
	raw = append(raw, make([]byte, 16)...)
	report, err := InspectLegacyRoom(raw)
	if err != nil {
		t.Fatal(err)
	}
	if len(report.Issues) != 1 || report.Issues[0].Kind != "missing-text-terminator" || len(report.Room.Objects[0].Name) != 80 || report.Room.Objects[0].Description != "neighbor" {
		t.Fatal("unbounded or unreported text read")
	}
	if _, err := DecodeLegacyRoom(raw); err == nil {
		t.Fatal("strict accepted unterminated field")
	}
}

func FuzzLegacyInspection(f *testing.F) {
	for _, name := range []string{"r00001", "r00100", "r00173"} {
		raw, err := os.ReadFile("../../../rooms/r00/" + name)
		if err != nil {
			f.Fatal(err)
		}
		f.Add(raw)
	}
	f.Fuzz(func(t *testing.T, raw []byte) {
		report, err := InspectLegacyRoom(raw)
		if err != nil {
			return
		}
		if !bytes.Equal(report.Source, raw) || report.SHA256 != sha256.Sum256(raw) || report.Consumed > len(raw) {
			t.Fatal("lost inspection evidence")
		}
	})
}
