package world

import (
	"errors"
	"fmt"
	"strings"
	"unicode/utf8"
)

// CanonicalRoomObject is the small identity/view pair admitted by target
// inspection.  ID is the durable ItemCollection identity; Object is copied so
// callers cannot mutate the committed snapshot while rendering a response.
type CanonicalRoomObject struct {
	ID     string
	Object LegacyObject
}

var (
	// ErrCanonicalRoomObjectUnavailable distinguishes an un-migrated legacy
	// floor from an ordinary name miss.  A caller must not use Resource.Objects
	// as a fallback when this error is returned.
	ErrCanonicalRoomObjectUnavailable = errors.New("canonical room object root unavailable")
	ErrCanonicalRoomObjectNotFound    = errors.New("canonical room object root not found")
	ErrAmbiguousCanonicalRoomObject   = errors.New("ambiguous canonical room object root")
)

// SelectCanonicalRoomObjectRoot resolves one exact, visible floor root. It
// intentionally does not inspect nested Contents, occurrence numbers, keys,
// prefixes, or LegacyRoom.Objects. A nil collection with legacy floor data is
// an unresolved migration boundary, not an empty room.
func SelectCanonicalRoomObjectRoot(items *ItemCollection, name string, detectInvisible bool) (CanonicalRoomObject, error) {
	if items == nil {
		return CanonicalRoomObject{}, ErrCanonicalRoomObjectUnavailable
	}
	if err := items.Validate(); err != nil {
		return CanonicalRoomObject{}, fmt.Errorf("%w: %v", ErrCanonicalRoomObjectUnavailable, err)
	}
	name = strings.TrimSpace(name)
	if name == "" || !utf8.ValidString(name) {
		return CanonicalRoomObject{}, fmt.Errorf("canonical room object name required")
	}
	found := false
	var selected CanonicalRoomObject
	for _, id := range items.Inventory {
		item, ok := items.Items[id]
		if !ok || id == "" {
			return CanonicalRoomObject{}, fmt.Errorf("%w: missing root %q", ErrCanonicalRoomObjectUnavailable, id)
		}
		if item.Object.Name == "" || !utf8.ValidString(item.Object.Name) {
			return CanonicalRoomObject{}, fmt.Errorf("%w: invalid root name", ErrCanonicalRoomObjectUnavailable)
		}
		if item.Object.Name != name {
			continue
		}
		if flag(item.Object.Flags[:], objectInvisibleFlag) && !detectInvisible {
			continue
		}
		if found {
			return CanonicalRoomObject{}, ErrAmbiguousCanonicalRoomObject
		}
		found = true
		selected = CanonicalRoomObject{ID: id, Object: item.Object}
	}
	if !found {
		return CanonicalRoomObject{}, fmt.Errorf("%w: %q", ErrCanonicalRoomObjectNotFound, name)
	}
	return selected, nil
}

type ObjectListing struct {
	Text      string
	Truncated bool
}

// ListRoomObjects ports list_obj/obj_str in existing linked-list order. It
// groups adjacent visible names (and adjustments when detecting magic).
// Adjustment is a signed ILP32 char; the migration DTO retains its raw byte.
// Unlike obj_str it never trims the stored name in place.
func ListRoomObjects(objects []LegacyObject, detectInvisible, detectMagic bool) ObjectListing {
	visible := func(o LegacyObject) bool {
		return !flag(o.Flags[:], 1) && !flag(o.Flags[:], 18) && (detectInvisible || !flag(o.Flags[:], 2))
	}
	var out strings.Builder
	truncated := false
	for i := 0; i < len(objects); i++ {
		// Original list_obj tests the buffer (including final comma) before each group.
		if out.Len() >= 1970 {
			truncated = true
			break
		}
		if !visible(objects[i]) {
			continue
		}
		count := 1
		for i+1 < len(objects) && objects[i+1].Name == objects[i].Name && (!detectMagic || objects[i+1].Adjustment == objects[i].Adjustment) && visible(objects[i+1]) {
			i++
			count++
		}
		if count > 1 {
			fmt.Fprintf(&out, "(x%d) ", count)
		}
		o := objects[i]
		out.WriteString(strings.TrimRight(o.Name, " "))
		if detectMagic {
			if o.Adjustment != 0 {
				fmt.Fprintf(&out, "(%+d)", int8(o.Adjustment))
			} else if o.MagicPower != 0 {
				out.WriteString("(주문)")
			}
		}
		out.WriteString(", ")
	}
	return ObjectListing{Text: strings.TrimSuffix(out.String(), ", "), Truncated: truncated}
}

// legacySubjectParticle matches under_han's 511-byte temporary buffer and
// optional final parenthesized suffix stripping, including invalid UTF-8→가.
// Preserving this quirk is explicit; changing grammar is a separate decision.
func legacySubjectParticle(text string) string {
	if len(text) > 511 {
		text = text[:511]
	}
	if strings.HasSuffix(text, ")") {
		if at := strings.LastIndexByte(text, '('); at >= 0 {
			text = text[:at]
		}
	}
	if utf8.ValidString(text) {
		last, _ := utf8.DecodeLastRuneInString(text)
		if last >= 0xac00 && last <= 0xd7a3 && (last-0xac00)%28 != 0 {
			return "이"
		}
	}
	return "가"
}

// RenderRoomObjects is the floor-items section only; callers must first apply
// room visibility. Truncation metadata is available through ListRoomObjects.
func RenderRoomObjects(objects []LegacyObject, detectInvisible, detectMagic bool) string {
	listing := ListRoomObjects(objects, detectInvisible, detectMagic)
	if listing.Text == "" {
		return ""
	}
	return listing.Text + legacySubjectParticle(listing.Text) + " 놓여져 있습니다.\n"
}
