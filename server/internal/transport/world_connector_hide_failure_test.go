package transport

import (
	"context"
	"strings"
	"testing"
)

func TestWorldConnectorSubmitDispatchesFailedHideEventWithoutLeakingToActor(t *testing.T) {
	store := connectorHideFixture(t)
	connector, actor, target, observer := connectorThreeWorldConnections(t, store, "hide-failure-world")
	connector.config.Roll = func(_, _ int) int { return 100 }

	text, err := actor.Submit(context.Background(), "숨겨")
	if err != nil || text != "당신은 애써 숨어보려고 합니다." {
		t.Fatalf("hide failure response=%q err=%v", text, err)
	}
	for name, connection := range map[string]*worldConnection{"target": target, "observer": observer} {
		select {
		case event := <-connection.events:
			if !strings.Contains(event, "Alice") || !strings.Contains(event, "애써 숨어보려고 합니다") || strings.Contains(event, "그림자 사이로 숨었습니다") {
				t.Fatalf("%s hide failure event=%q", name, event)
			}
		default:
			t.Fatalf("%s hide failure event missing", name)
		}
	}
	select {
	case event := <-actor.events:
		t.Fatalf("actor received own failed hide event=%q", event)
	default:
	}
	if store.commits != 1 {
		t.Fatalf("commits=%d", store.commits)
	}
}
