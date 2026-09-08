package world

import (
	"fmt"
	"strings"
	"unicode/utf8"
)

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
