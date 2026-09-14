package world

import (
	"reflect"
	"testing"
)

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
