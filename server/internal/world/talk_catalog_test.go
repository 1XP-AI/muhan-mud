package world

import (
	"errors"
	"reflect"
	"strings"
	"testing"
	"testing/fstest"

	"golang.org/x/text/encoding/korean"
)

func TestLoadTalkCatalogParsesCRecordsAndActions(t *testing.T) {
	fsys := fstest.MapFS{
		"Guide-7": &fstest.MapFile{Data: []byte(strings.Join([]string{
			"hello",
			"Welcome, traveller.",
			"fight ATTACK",
			"You should not have asked.",
			"mood ACTION smile PLAYER",
			"The guide smiles.",
			"heal CAST cure PLAYER",
			"A warm light surrounds you.",
			"gift GIVE 107",
			"Take this.",
			"wizard's",
			"The apostrophe is not part of the C key token.",
			"hello",
			"The first duplicate wins.",
			"",
			"",
		}, "\n"))},
	}

	catalog, err := LoadTalkCatalog(fsys)
	if err != nil {
		t.Fatal(err)
	}
	if got := catalog.Len(); got != 1 {
		t.Fatalf("catalog length=%d want 1", got)
	}
	file, ok, err := catalog.Lookup("Guide", 7)
	if err != nil || !ok {
		t.Fatalf("lookup file=%+v ok=%v err=%v", file, ok, err)
	}
	if file.Path != "Guide-7" || len(file.Topics) != 7 {
		t.Fatalf("file=%+v", file)
	}

	cases := []struct {
		key       string
		response  string
		action    TalkActionKind
		actionArg string
		target    string
	}{
		{key: "hello", response: "Welcome, traveller."},
		{key: "fight", response: "You should not have asked.", action: TalkActionAttack},
		{key: "mood", response: "The guide smiles.", action: TalkActionAction, actionArg: "smile", target: "PLAYER"},
		{key: "heal", response: "A warm light surrounds you.", action: TalkActionCast, actionArg: "cure", target: "PLAYER"},
		{key: "gift", response: "Take this.", action: TalkActionGive, actionArg: "107"},
		{key: "wizard", response: "The apostrophe is not part of the C key token."},
	}
	for i, want := range cases {
		got := file.Topics[i]
		if got.Key != want.key || got.Response != want.response || got.Action.Kind != want.action || got.Action.Name != want.actionArg || got.Action.Target != want.target {
			t.Fatalf("topic[%d]=%+v want key=%q response=%q action=%v name=%q target=%q", i, got, want.key, want.response, want.action, want.actionArg, want.target)
		}
	}
	got, ok := file.Topic("hello")
	if !ok || got.Response != "Welcome, traveller." {
		t.Fatalf("first duplicate lookup=%+v ok=%v", got, ok)
	}
}

func TestLoadTalkCatalogDecodesCP949BodiesAndKeepsCopiesOwned(t *testing.T) {
	encoded, err := korean.EUCKR.NewEncoder().Bytes([]byte("임무\n응답입니다.\n"))
	if err != nil {
		t.Fatal(err)
	}
	catalog, err := LoadTalkCatalog(fstest.MapFS{
		"계석치무-25": &fstest.MapFile{Data: encoded},
	})
	if err != nil {
		t.Fatal(err)
	}
	file, ok, err := catalog.Lookup("계석치무", 25)
	if err != nil || !ok || len(file.Topics) != 1 {
		t.Fatalf("lookup=%+v ok=%v err=%v", file, ok, err)
	}
	if file.Topics[0].Key != "임무" || file.Topics[0].Response != "응답입니다." {
		t.Fatalf("decoded topic=%+v", file.Topics[0])
	}
	file.Topics[0].Response = "mutated"
	again, ok, err := catalog.Lookup("계석치무", 25)
	if err != nil || !ok || again.Topics[0].Response != "응답입니다." {
		t.Fatalf("catalog exposed mutable data: %+v ok=%v err=%v", again, ok, err)
	}
}

func TestLoadTalkCatalogIgnoresNonAddressableFilesAndRejectsCorruptRecords(t *testing.T) {
	catalog, err := LoadTalkCatalog(fstest.MapFS{
		"Guide-7":           &fstest.MapFile{Data: []byte("hello\nanswer\n")},
		"priest":            &fstest.MapFile{Data: []byte("mail draft")},
		"The_Town_Crier-1y": &fstest.MapFile{Data: []byte("news\nanswer\n")},
		"nested/Guide-8":    &fstest.MapFile{Data: []byte("hello\nanswer\n")},
	})
	if err != nil {
		t.Fatal(err)
	}
	if catalog.Len() != 1 {
		t.Fatalf("catalog length=%d want 1", catalog.Len())
	}
	ignored := catalog.IgnoredPaths()
	if !reflect.DeepEqual(ignored, []string{"The_Town_Crier-1y", "nested/Guide-8", "priest"}) {
		t.Fatalf("ignored=%v", ignored)
	}

	for name, data := range map[string][]byte{
		"Guide-7": []byte("odd\n"),
		"Guide-8": []byte("hello\nanswer\nsecond-key\n"),
		"Guide-9": []byte("hello\n\xff\n"),
	} {
		if _, err := LoadTalkCatalog(fstest.MapFS{name: &fstest.MapFile{Data: data}}); err == nil {
			t.Fatalf("corrupt file %q unexpectedly loaded", name)
		}
	}
	if _, err := LoadTalkCatalog(fstest.MapFS{"priest": &fstest.MapFile{Data: []byte("draft")}}); !errors.Is(err, ErrTalkCatalogEmpty) {
		t.Fatalf("empty catalog err=%v", err)
	}
}

func TestTalkCatalogLookupRejectsUnsafeNPCIdentity(t *testing.T) {
	catalog, err := LoadTalkCatalog(fstest.MapFS{
		"Guide-7": &fstest.MapFile{Data: []byte("hello\nanswer\n")},
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"", "Guide/7", "../Guide", " Guide", "Guide ", "Guide\n7"} {
		if _, _, err := catalog.Lookup(name, 7); err == nil {
			t.Fatalf("unsafe NPC name %q accepted", name)
		}
	}
	if _, _, err := catalog.Lookup("Guide", -1); err == nil {
		t.Fatal("negative NPC level accepted")
	}
	if _, _, err := catalog.Lookup("Guide", 256); err == nil {
		t.Fatal("out-of-range NPC level accepted")
	}
}
