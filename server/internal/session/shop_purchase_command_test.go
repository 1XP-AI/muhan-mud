package session

import "testing"

func TestParseShopPurchaseLineAdmitsOnlyBoundedAliasesAndPositiveOccurrence(t *testing.T) {
	tests := []struct {
		line       string
		name       string
		occurrence int
	}{
		{line: "사 검", name: "검", occurrence: 1},
		{line: `구입 "마법 검" 2`, name: "마법 검", occurrence: 2},
	}
	for _, tc := range tests {
		got, ok := ParseShopPurchaseLine(tc.line)
		if !ok || got.Name != tc.name || got.Occurrence != tc.occurrence {
			t.Fatalf("ParseShopPurchaseLine(%q)=%+v,%t want name=%q occurrence=%d", tc.line, got, ok, tc.name, tc.occurrence)
		}
	}
	for _, line := range []string{
		"사",
		"구입",
		"buy 검",
		"사 검 0",
		"사 검 -1",
		"사 검 x",
		"사 검 1 extra",
		"사 검\n다음",
		"사 검\x00",
	} {
		if _, ok := ParseShopPurchaseLine(line); ok {
			t.Fatalf("unsupported shop purchase line accepted: %q", line)
		}
	}

	for _, line := range []string{"사 검", "구입 검 2"} {
		parsed, err := ParseCommand(line)
		if err != nil || parsed.Kind != CommandShopPurchase {
			t.Fatalf("ParseCommand(%q)=%+v err=%v", line, parsed, err)
		}
	}
}
