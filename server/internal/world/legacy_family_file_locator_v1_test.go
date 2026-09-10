package world

import (
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"testing"
)

func makeLegacyFamilyTree(t *testing.T, list string, members map[int16]string) string {
	t.Helper()
	root := t.TempDir()
	familyDir := filepath.Join(root, "family")
	if err := os.Mkdir(familyDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(familyDir, "family_list"), []byte(list), 0o600); err != nil {
		t.Fatal(err)
	}
	for id, content := range members {
		if err := os.WriteFile(filepath.Join(familyDir, "family_member_"+strconv.Itoa(int(id))), []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Chmod(root, 0o700); err != nil {
		t.Fatal(err)
	}
	return root
}

func TestLocateLegacyFamilyRawFilesV1AndInspectWithExplicitIDs(t *testing.T) {
	root := makeLegacyFamilyTree(t,
		"1 Red Boss 3\n16 end end 0\n",
		map[int16]string{1: "5 Boss\n4 Alice\n0 Red\n"})
	located, err := LocateLegacyFamilyRawFilesV1(root)
	if err != nil {
		t.Fatal(err)
	}
	if located.FamilyList.Metadata.RelativePath != filepath.Join("family", "family_list") || located.FamilyList.SHA256 == [32]byte{} {
		t.Fatalf("unexpected family list source metadata: %+v", located.FamilyList.Metadata)
	}
	inspection, err := InspectLegacyFamilyRawFilesV1(located, LegacyFamilyIdentityMapV1{Families: map[int16]LegacyFamilyIdentityFamilyV1{
		1: {BossID: "boss-id", Members: map[string]string{"Boss": "boss-id", "Alice": "alice-id"}},
	}})
	if err != nil {
		t.Fatal(err)
	}
	if inspection.Catalog.Families[1].Fee != 3 || inspection.Catalog.Families[1].Boss != "Boss" || inspection.BossIDs[1] != "boss-id" {
		t.Fatalf("unexpected inspection catalog: %+v", inspection.Catalog)
	}
	members := inspection.Family.Members[1]
	if len(members) != 2 || members[0].ID != "boss-id" || members[0].Class != 5 || members[1].ID != "alice-id" || inspection.SHA256 == [32]byte{} {
		t.Fatalf("unexpected inspection family=%+v digest=%x", inspection.Family, inspection.SHA256)
	}
	clone := located.Clone()
	clone.FamilyList.Source[0] = 'X'
	if located.FamilyList.Source[0] == 'X' {
		t.Fatal("clone aliases family source")
	}
}

func TestLegacyFamilyLocatorRejectsUnsafeTreeAndUnexpectedEntries(t *testing.T) {
	root := makeLegacyFamilyTree(t, "1 Red Boss 3\n16 end end 0\n", map[int16]string{1: "5 Boss\n0 Red\n"})
	familyDir := filepath.Join(root, "family")
	cases := []struct {
		name   string
		want   error
		mutate func(t *testing.T)
	}{
		{name: "root mode", want: ErrLegacyFamilyFileLocatorUnsafe, mutate: func(t *testing.T) { _ = os.Chmod(root, 0o750) }},
		{name: "family mode", want: ErrLegacyFamilyFileLocatorUnsafe, mutate: func(t *testing.T) { _ = os.Chmod(familyDir, 0o750) }},
		{name: "list mode", want: ErrLegacyFamilyFileLocatorUnsafe, mutate: func(t *testing.T) { _ = os.Chmod(filepath.Join(familyDir, "family_list"), 0o640) }},
		{name: "list symlink", want: ErrLegacyFamilyFileLocatorUnsafe, mutate: func(t *testing.T) {
			path := filepath.Join(familyDir, "family_list")
			_ = os.Remove(path)
			_ = os.Symlink(filepath.Join(t.TempDir(), "other"), path)
		}},
		{name: "unexpected entry", want: ErrLegacyFamilyFileLocatorUnsafe, mutate: func(t *testing.T) {
			_ = os.WriteFile(filepath.Join(familyDir, "family_member_2"), []byte("0 Blue\n"), 0o600)
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			caseRoot := makeLegacyFamilyTree(t, "1 Red Boss 3\n16 end end 0\n", map[int16]string{1: "5 Boss\n0 Red\n"})
			tc.mutate = func(t *testing.T) {
				// Reapply the mutation against this subtest's fresh tree.
				caseFamily := filepath.Join(caseRoot, "family")
				switch tc.name {
				case "root mode":
					_ = os.Chmod(caseRoot, 0o750)
				case "family mode":
					_ = os.Chmod(caseFamily, 0o750)
				case "list mode":
					_ = os.Chmod(filepath.Join(caseFamily, "family_list"), 0o640)
				case "list symlink":
					path := filepath.Join(caseFamily, "family_list")
					_ = os.Remove(path)
					_ = os.Symlink(filepath.Join(t.TempDir(), "other"), path)
				case "unexpected entry":
					_ = os.WriteFile(filepath.Join(caseFamily, "family_member_2"), []byte("0 Blue\n"), 0o600)
				}
			}
			tc.mutate(t)
			if _, err := LocateLegacyFamilyRawFilesV1(caseRoot); !errors.Is(err, tc.want) && !errors.Is(err, ErrLegacyFamilyFileLocatorChanged) {
				t.Fatalf("error=%v, want errors.Is(...,%v)", err, tc.want)
			}
		})
	}
}

func TestLegacyFamilyParsingFailsClosed(t *testing.T) {
	baseSource := func(list, member string) LegacyFamilyRawFilesV1 {
		return LegacyFamilyRawFilesV1{
			FamilyList:  LegacyFamilyFileSourceV1{Source: []byte(list)},
			MemberFiles: map[int16]LegacyFamilyFileSourceV1{1: {Source: []byte(member)}},
		}
	}
	identityMap := LegacyFamilyIdentityMapV1{Families: map[int16]LegacyFamilyIdentityFamilyV1{1: {BossID: "boss-id", Members: map[string]string{"Boss": "boss-id"}}}}
	cases := []struct {
		name   string
		source LegacyFamilyRawFilesV1
		want   error
	}{
		{name: "missing list sentinel", source: baseSource("1 Red Boss 3\n", "5 Boss\n0 Red\n"), want: ErrLegacyFamilySourceIncomplete},
		{name: "missing member sentinel", source: baseSource("1 Red Boss 3\n16 end end 0\n", "5 Boss\n"), want: ErrLegacyFamilySourceIncomplete},
		{name: "sentinel mismatch", source: baseSource("1 Red Boss 3\n16 end end 0\n", "5 Boss\n0 Blue\n"), want: ErrLegacyFamilySourceMalformed},
		{name: "unmapped member", source: baseSource("1 Red Boss 3\n16 end end 0\n", "5 Boss\n4 Alice\n0 Red\n"), want: ErrLegacyFamilyIdentityMapInvalid},
		{name: "duplicate family", source: baseSource("1 Red Boss 3\n1 Blue Other 2\n16 end end 0\n", "5 Boss\n0 Red\n"), want: ErrLegacyFamilySourceMalformed},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := InspectLegacyFamilyRawFilesV1(tc.source, identityMap)
			if !errors.Is(err, tc.want) {
				t.Fatalf("error=%v, want errors.Is(...,%v)", err, tc.want)
			}
		})
	}
}

func TestLegacyFamilyIdentityMapRejectsNameOnlyOrForeignRows(t *testing.T) {
	source := LegacyFamilyRawFilesV1{FamilyList: LegacyFamilyFileSourceV1{Source: []byte("1 Red Boss 3\n16 end end 0\n")}, MemberFiles: map[int16]LegacyFamilyFileSourceV1{1: {Source: []byte("5 Boss\n0 Red\n")}}}
	cases := []LegacyFamilyIdentityMapV1{
		{Families: map[int16]LegacyFamilyIdentityFamilyV1{1: {BossID: "", Members: map[string]string{"Boss": "boss-id"}}}},
		{Families: map[int16]LegacyFamilyIdentityFamilyV1{1: {BossID: "boss-id", Members: map[string]string{"Boss": ""}}}},
		{Families: map[int16]LegacyFamilyIdentityFamilyV1{1: {BossID: "boss-id", Members: map[string]string{"boss": "boss-id"}}}},
		{Families: map[int16]LegacyFamilyIdentityFamilyV1{1: {BossID: "boss-id", Members: map[string]string{"Boss": "boss-id"}}, 2: {BossID: "other", Members: map[string]string{}}}},
	}
	for index, mapping := range cases {
		if _, err := InspectLegacyFamilyRawFilesV1(source, mapping); !errors.Is(err, ErrLegacyFamilyIdentityMapInvalid) {
			t.Errorf("case %d error=%v, want identity map rejection", index, err)
		}
	}
}
