package world

import (
	"fmt"
	"io/fs"
)

// Template files are arrays of fixed structs, unlike recursive room records.
// Reuse the semantic field decoder but never read children from pointer slots.
func DecodeLegacyMonsterTemplate(raw []byte) (LegacyMonster, error) {
	if len(raw) != 1184 {
		return LegacyMonster{}, ErrLegacyRoom
	}
	r := roomReader{raw: raw, template: true}
	m := r.monster()
	if r.err != nil {
		return LegacyMonster{}, r.err
	}
	return m, nil
}

func DecodeLegacyObjectTemplate(raw []byte) (LegacyObject, error) {
	if len(raw) != 352 {
		return LegacyObject{}, ErrLegacyRoom
	}
	r := roomReader{raw: raw, template: true}
	o := r.object(0)
	if r.err != nil {
		return LegacyObject{}, r.err
	}
	return o, nil
}

// TemplateCatalog reads only the original 100-record mNN/oNN tables from an
// explicitly supplied filesystem root. No player files or runtime C calls.
// Decoded values own their strings/slices; no shared mutable template cache.
type TemplateCatalog struct{ FS fs.FS }

func (c TemplateCatalog) record(kind string, id int16, size int) ([]byte, error) {
	if id < 0 || c.FS == nil {
		return nil, fmt.Errorf("invalid template index or filesystem")
	}
	path := fmt.Sprintf("%s%02d", kind, int(id)/100)
	raw, err := fs.ReadFile(c.FS, path)
	if err != nil {
		return nil, fmt.Errorf("template %d: %w", id, err)
	}
	if len(raw) != 100*size {
		return nil, fmt.Errorf("template table %s has invalid length %d", path, len(raw))
	}
	start := (int(id) % 100) * size
	return raw[start : start+size], nil
}

func (c TemplateCatalog) Monster(id int16) (LegacyMonster, error) {
	raw, err := c.record("m", id, 1184)
	if err != nil {
		return LegacyMonster{}, err
	}
	return DecodeLegacyMonsterTemplate(raw)
}
func (c TemplateCatalog) Object(id int16) (LegacyObject, error) {
	raw, err := c.record("o", id, 352)
	if err != nil {
		return LegacyObject{}, err
	}
	return DecodeLegacyObjectTemplate(raw)
}
