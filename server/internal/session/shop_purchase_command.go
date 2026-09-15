package session

import (
	"errors"
	"strconv"
	"strings"
	"unicode/utf8"
)

// ErrUnsupportedShopPurchaseLine is returned before a receipt exists when a
// line is outside the bounded shop purchase contract. Merchant-NPC purchase
// and implicit client-provided stock IDs are deliberately not accepted by this
// parser; the bounded selector is resolved by the world reducer.
var ErrUnsupportedShopPurchaseLine = errors.New("line is not an implemented shop purchase command")

// ShopPurchaseCommand is the only client-facing purchase identity. The
// authoritative stock ID is resolved from the committed world snapshot by the
// transport; a terminal client never supplies or chooses that ID.
type ShopPurchaseCommand struct {
	Name       string
	Occurrence int
}

// ParseShopPurchaseLine admits the exact C aliases for the bounded shop slice:
// `사` / `구입` with no name (command7.c:buy cmnd->num < 2) and
// `사 <stock selector> [positive occurrence]` / `구입` with the same shape.
// The world reducer applies the source display-name/key prefix and visibility
// contract; this parser only preserves one bounded selector token.
func ParseShopPurchaseLine(line string) (ShopPurchaseCommand, bool) {
	if !utf8.ValidString(line) {
		return ShopPurchaseCommand{}, false
	}
	for _, r := range line {
		switch r {
		case '\r', '\n', '\x00', '\x1b', '\u2028', '\u2029':
			return ShopPurchaseCommand{}, false
		}
	}
	tokens, err := tokenizeLegacy(strings.TrimSpace(line))
	if err != nil || len(tokens) < 1 || len(tokens) > 3 {
		return ShopPurchaseCommand{}, false
	}
	if tokens[0] != "사" && tokens[0] != "구입" {
		return ShopPurchaseCommand{}, false
	}
	command := ShopPurchaseCommand{Occurrence: 1}
	if len(tokens) == 1 {
		return command, true
	}
	name := strings.TrimSpace(tokens[1])
	if name == "" {
		return ShopPurchaseCommand{}, false
	}
	command.Name = name
	if len(tokens) == 3 {
		occurrence, ok := positiveShopPurchaseOccurrence(tokens[2])
		if !ok {
			return ShopPurchaseCommand{}, false
		}
		command.Occurrence = occurrence
	}
	return command, true
}

// ShopPurchaseLineText is a descriptive alias for callers that parse a line
// before transport dispatch. It returns display-name input only, never a stock
// identity owned by the server.
func ShopPurchaseLineText(line string) (ShopPurchaseCommand, bool) {
	return ParseShopPurchaseLine(line)
}

func positiveShopPurchaseOccurrence(token string) (int, bool) {
	if token == "" {
		return 0, false
	}
	for _, r := range token {
		if r < '0' || r > '9' {
			return 0, false
		}
	}
	value, err := strconv.ParseUint(token, 10, 31)
	if err != nil || value < 1 {
		return 0, false
	}
	return int(value), true
}
