package world

import "testing"

func TestQuoteShopPurchaseByNameRequiresExactNameAndPositiveOccurrence(t *testing.T) {
	s := shopTransactionFixture(t)
	quote, err := s.QuoteShopPurchaseByName("a", "검", 2)
	if err != nil {
		t.Fatal(err)
	}
	if quote.StockID != "stock-sword-2" || quote.ItemName != "검" || quote.Price != 60 {
		t.Fatalf("quote=%+v", quote)
	}
	for _, tc := range []struct {
		name string
		occ  int
	}{
		{name: "검", occ: 0},
		{name: "검", occ: 3},
		{name: "검술", occ: 1}, // prefix is not accepted
	} {
		if _, err := s.QuoteShopPurchaseByName("a", tc.name, tc.occ); err == nil {
			t.Fatalf("invalid name/occurrence accepted: %+v", tc)
		}
	}
	if _, err := s.QuoteShopPurchaseByName("a", "검", 1); err != nil {
		t.Fatal(err)
	}
}
