package world

import (
	"encoding/binary"
	"os"
	"testing"
	"testing/fstest"
)

func TestRawTemplateHasNoRecursiveInventory(t *testing.T) {
	b := make([]byte, 1184)
	copy(b, "wolf")
	binary.LittleEndian.PutUint16(b[332:], 42)
	for i := 1160; i < 1184; i++ {
		b[i] = 255
	}
	m, err := DecodeLegacyMonsterTemplate(b)
	if err != nil || m.Name != "wolf" || m.HPMax != 42 || len(m.Inventory) != 0 {
		t.Fatalf("%+v %v", m, err)
	}
	o := make([]byte, 352)
	copy(o, "sword")
	for i := 336; i < 352; i++ {
		o[i] = 255
	}
	obj, err := DecodeLegacyObjectTemplate(o)
	if err != nil || obj.Name != "sword" || len(obj.Contents) != 0 {
		t.Fatalf("%+v %v", obj, err)
	}
	if _, err := DecodeLegacyObjectTemplate(append(o, 0, 0, 0, 0)); err == nil {
		t.Fatal("room object accepted as template")
	}
}

func TestTemplateCatalogIndex(t *testing.T) {
	data := make([]byte, 352*100)
	copy(data[352:], "second")
	catalog := TemplateCatalog{FS: fstest.MapFS{"o01": &fstest.MapFile{Data: data}}}
	obj, err := catalog.Object(101)
	if err != nil || obj.Name != "second" {
		t.Fatalf("%+v %v", obj, err)
	}
	if _, err := catalog.Object(-1); err == nil {
		t.Fatal("negative index accepted")
	}
	if _, err := catalog.Object(201); err == nil {
		t.Fatal("missing catalog accepted")
	}
}

func TestOriginalTemplateCatalog(t *testing.T) {
	catalog := TemplateCatalog{FS: os.DirFS("../../../objmon")}
	m, err := catalog.Monster(1)
	if err != nil {
		t.Fatal(err)
	}
	o, err := catalog.Object(1)
	if err != nil {
		t.Fatal(err)
	}
	if m.Name != "노동자" || o.Name != "단도" {
		t.Fatal("unexpected original template 1")
	}
	t.Logf("original template 1: monster=%q object=%q", m.Name, o.Name)
}
