package world

import (
	"reflect"
	"testing"
)

func TestQuoteShopPurchaseByNameRequiresPositiveOccurrenceAndRejectsNonMatchingSelector(t *testing.T) {
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
		{name: "검술", occ: 1}, // no stock display-name/key has this prefix
	} {
		if _, err := s.QuoteShopPurchaseByName("a", tc.name, tc.occ); err == nil {
			t.Fatalf("invalid name/occurrence accepted: %+v", tc)
		}
	}
	if _, err := s.QuoteShopPurchaseByName("a", "검", 1); err != nil {
		t.Fatal(err)
	}
}

func TestShopPurchaseByNameMatchesLegacyDisplayAndKeyPrefixesInCanonicalOrder(t *testing.T) {
	s := shopTransactionFixture(t)
	room := s.Rooms[101]
	permanent := room.Items.Items["stock-sword"].Object.Flags
	room.Items.Items["stock-display-prefix"] = Item{Object: LegacyObject{Name: "장검", Value: 70, Weight: 1, Flags: permanent}}
	room.Items.Items["stock-key-zero"] = Item{Object: LegacyObject{Name: "도구0", Keys: [3]string{"needle-zero", "", ""}, Value: 71, Weight: 1, Flags: permanent}}
	room.Items.Items["stock-key-one"] = Item{Object: LegacyObject{Name: "도구1", Keys: [3]string{"", "needle-one", ""}, Value: 72, Weight: 1, Flags: permanent}}
	room.Items.Items["stock-key-two"] = Item{Object: LegacyObject{Name: "도구2", Keys: [3]string{"", "", "needle-two"}, Value: 73, Weight: 1, Flags: permanent}}
	room.Items.Inventory = append(room.Items.Inventory, "stock-display-prefix", "stock-key-zero", "stock-key-one", "stock-key-two")
	s.Rooms[101] = room

	for _, tc := range []struct {
		selector string
		wantID   string
	}{
		{selector: "장", wantID: "stock-display-prefix"},
		{selector: "needle-zero", wantID: "stock-key-zero"},
		{selector: "needle-one", wantID: "stock-key-one"},
		{selector: "needle-two", wantID: "stock-key-two"},
	} {
		t.Run(tc.selector, func(t *testing.T) {
			quote, err := s.QuoteShopPurchaseByName("a", tc.selector, 1)
			if err != nil {
				t.Fatal(err)
			}
			if quote.StockID != tc.wantID {
				t.Fatalf("selector=%q quote=%+v want stock=%q", tc.selector, quote, tc.wantID)
			}
		})
	}

	quote, err := s.QuoteShopPurchaseByName("a", "검", 2)
	if err != nil || quote.StockID != "stock-sword-2" {
		t.Fatalf("duplicate display-name occurrence quote=%+v err=%v", quote, err)
	}
	next, result, err := s.BuyShopItemByName("a", "needle-one", 1, sequenceShopAllocator("owned-key-one"))
	if err != nil || result.StockID != "stock-key-one" || next.Players["a"].Body.Gold != 28 {
		t.Fatalf("key-prefix buy next=%+v result=%+v err=%v", next.Players["a"], result, err)
	}

	called := false
	_, result, err = s.BuyShopItemByName("a", "does-not-match", 1, func() (string, error) {
		called = true
		return "unexpected", nil
	})
	if err != nil || result.Action != ShopPurchaseNotSoldAction || called {
		t.Fatalf("non-matching selector result=%+v err=%v allocator=%t", result, err, called)
	}
}

func TestShopPurchaseByNameSkipsInvisibleMatchingRootsUnlessDetectInvisible(t *testing.T) {
	s := shopTransactionFixture(t)
	room := s.Rooms[101]
	invisible := [8]byte{}
	invisible[objectInvisibleFlag/8] |= 1 << (objectInvisibleFlag % 8)
	room.Items.Items["stock-hidden-needle"] = Item{Object: LegacyObject{Name: "숨은물건", Keys: [3]string{"needle", "", ""}, Value: 70, Weight: 1, Flags: invisible}}
	room.Items.Items["stock-visible-needle"] = Item{Object: LegacyObject{Name: "보이는물건", Keys: [3]string{"needle", "", ""}, Value: 71, Weight: 1}}
	room.Items.Inventory = append(room.Items.Inventory, "stock-hidden-needle", "stock-visible-needle")
	s.Rooms[101] = room

	quote, err := s.QuoteShopPurchaseByName("a", "needle", 1)
	if err != nil || quote.StockID != "stock-visible-needle" {
		t.Fatalf("invisible root was not skipped quote=%+v err=%v", quote, err)
	}
	actor := s.Players["a"]
	actor.Body.Flags[playerDetectInvisibleFlag/8] |= 1 << (playerDetectInvisibleFlag % 8)
	s.Players["a"] = actor
	quote, err = s.QuoteShopPurchaseByName("a", "needle", 1)
	if err != nil || quote.StockID != "stock-hidden-needle" {
		t.Fatalf("PDINVI did not admit first invisible root quote=%+v err=%v", quote, err)
	}
}

func TestBuyShopItemByNamePrintsCAskWhatWhenNameMissing(t *testing.T) {
	s := shopTransactionFixture(t)
	original := s.clone()
	called := false
	next, result, err := s.BuyShopItemByName("a", "", 1, func() (string, error) {
		called = true
		return "should-not-allocate", nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Action != ShopPurchaseAskWhatAction || result.Response != ShopPurchaseAskWhatResponse || result.ShopRoomID != 100 || result.Event != nil {
		t.Fatalf("result=%+v", result)
	}
	if called || !reflect.DeepEqual(next, original) || !reflect.DeepEqual(s, original) {
		t.Fatal("ask-what buy cloned, allocated, or mutated")
	}
}

func TestBuyShopItemByNamePrintsCNotShopBeforeAskWhat(t *testing.T) {
	s := shopTransactionFixture(t)
	room := s.Rooms[100]
	room.Resource.Flags = [8]byte{}
	s.Rooms[100] = room
	result, err := func() (ShopPurchaseResult, error) {
		_, got, buyErr := s.BuyShopItemByName("a", "", 1, sequenceShopAllocator("unused"))
		return got, buyErr
	}()
	if err != nil || result.Response != ShopPurchaseNotShopResponse || result.Action != ShopPurchaseNotShopAction {
		t.Fatalf("bare buy outside shop=%+v err=%v", result, err)
	}
}

func TestBuyShopItemByNamePrintsCAskWhatBeforeMissingStorage(t *testing.T) {
	s := shopTransactionFixture(t)
	delete(s.Rooms, 101)
	next, result, err := s.BuyShopItemByName("a", "  ", 1, sequenceShopAllocator("unused"))
	if err != nil || result.Response != ShopPurchaseAskWhatResponse {
		t.Fatalf("bare buy missing storage=%+v err=%v", result, err)
	}
	if next.Players["a"].Body.Gold != 100 {
		t.Fatalf("ask-what debit gold=%d", next.Players["a"].Body.Gold)
	}
}

func TestBuyShopItemByNamePrintsCNoStockBeforeNotSold(t *testing.T) {
	s := shopTransactionFixture(t)
	delete(s.Rooms, 101)
	_, result, err := s.BuyShopItemByName("a", "검", 1, sequenceShopAllocator("unused"))
	if err != nil || result.Response != ShopPurchaseNoStockResponse || result.Action != ShopPurchaseNoStockAction {
		t.Fatalf("named buy missing storage=%+v err=%v", result, err)
	}
}

func TestBuyShopItemByNamePrintsCNotSoldWhenNameMissingFromStorage(t *testing.T) {
	s := shopTransactionFixture(t)
	original := s.clone()
	called := false
	next, result, err := s.BuyShopItemByName("a", "방패", 1, func() (string, error) {
		called = true
		return "should-not-allocate", nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Action != ShopPurchaseNotSoldAction || result.Response != ShopPurchaseNotSoldResponse || result.Event != nil {
		t.Fatalf("result=%+v", result)
	}
	if called || !reflect.DeepEqual(next, original) {
		t.Fatal("not-sold name buy cloned or allocated")
	}

	_, prefix, err := s.BuyShopItemByName("a", "검술", 1, sequenceShopAllocator("unused"))
	if err != nil || prefix.Response != ShopPurchaseNotSoldResponse {
		t.Fatalf("prefix name=%+v err=%v", prefix, err)
	}
	_, occ, err := s.BuyShopItemByName("a", "검", 3, sequenceShopAllocator("unused"))
	if err != nil || occ.Response != ShopPurchaseNotSoldResponse {
		t.Fatalf("missing occurrence=%+v err=%v", occ, err)
	}
}

func TestBuyShopItemByNameStillBuysExactStock(t *testing.T) {
	s := shopTransactionFixture(t)
	next, result, err := s.BuyShopItemByName("a", "검", 2, sequenceShopAllocator("bought-second"))
	if err != nil {
		t.Fatal(err)
	}
	if result.Action != "buy-shop-item" || result.StockID != "stock-sword-2" || result.GoldAfter != 40 || result.Event == nil {
		t.Fatalf("result=%+v", result)
	}
	if next.Players["a"].Body.Gold != 40 || !containsString(next.Players["a"].Items.Inventory, "bought-second") {
		t.Fatalf("player=%+v", next.Players["a"])
	}
}
