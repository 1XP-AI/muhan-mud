package engine

import (
	"reflect"
	"testing"
)

func aiGMProvenance() ContentProvenance {
	return ContentProvenance{
		Source:       ContentSourceAIGM,
		Model:        "gpt-5.6-luna",
		PromptDigest: "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef",
		Seed:         17,
	}
}

func engineContentProposal() ContentProposal {
	return ContentProposal{
		SchemaVersion: ContentSchemaVersion,
		WorldID:       "muhan-01",
		BaseRevision:  4,
		ProposalID:    "gm-0004",
		Provenance:    aiGMProvenance(),
		Operations: []ContentOperation{
			{
				Kind: ContentKindEvent, Action: ContentActionCreate, ID: "event-wolf-warning",
				Event: &EventSpec{ID: "event-wolf-warning", ScenarioID: "scenario-forest", Kind: EventKindMessage, AtSeconds: 30, RoomID: "room-forest-edge", Message: "숲 깊은 곳에서 울음소리가 들립니다."},
			},
			{
				Kind: ContentKindMap, Action: ContentActionCreate, ID: "map-forest",
				Map: &MapSpec{ID: "map-forest", Name: "북쪽 숲", Rooms: []RoomSpec{
					{ID: "room-forest-edge", MapID: "map-forest", Name: "숲 입구", MinLevel: 1, MaxLevel: 10, Exits: []ExitSpec{{ID: "exit-forest-north", Direction: "북", DestinationRoomID: "room-forest-depth"}}},
					{ID: "room-forest-depth", MapID: "map-forest", Name: "숲 깊은 곳", MinLevel: 3, MaxLevel: 15, Exits: []ExitSpec{{ID: "exit-forest-south", Direction: "남", DestinationRoomID: "room-forest-edge"}}},
				}},
			},
			{
				Kind: ContentKindMonsterTemplate, Action: ContentActionCreate, ID: "monster-wolf",
				MonsterTemplate: &MonsterTemplateSpec{ID: "monster-wolf", Name: "숲 늑대", MinLevel: 2, MaxLevel: 4, MinHP: 20, MaxHP: 40, XP: 80, MinGold: 2, MaxGold: 12, Tags: []string{"beast", "forest"}},
			},
			{
				Kind: ContentKindSpawnRule, Action: ContentActionCreate, ID: "spawn-forest-wolf",
				SpawnRule: &SpawnRuleSpec{ID: "spawn-forest-wolf", RoomID: "room-forest-depth", MonsterTemplateID: "monster-wolf", MinCount: 1, MaxCount: 3, RespawnSeconds: 300, LevelOffset: 0},
			},
			{
				Kind: ContentKindScenario, Action: ContentActionCreate, ID: "scenario-forest",
				Scenario: &ScenarioSpec{ID: "scenario-forest", Name: "숲의 첫 울음", StartRoomID: "room-forest-edge", MinLevel: 1, MaxLevel: 10, EventIDs: []string{"event-wolf-warning"}},
			},
		},
	}
}

func TestValidateContentProposalAcceptsAIGMContentAndReturnsDigest(t *testing.T) {
	report, err := ValidateContentProposal(engineContentProposal())
	if err != nil {
		t.Fatalf("ValidateContentProposal() error = %v", err)
	}
	if report.Risk != ContentRiskAdditive || report.OperationCount != 5 || report.RoomCount != 2 || report.EventCount != 1 || len(report.Digest) != 64 {
		t.Fatalf("unexpected validation report: %+v", report)
	}
}

func TestCanonicalContentDigestIgnoresOperationOrderWithoutMutatingInput(t *testing.T) {
	proposal := engineContentProposal()
	original := append([]ContentOperation(nil), proposal.Operations...)
	first, err := CanonicalContentDigest(proposal)
	if err != nil {
		t.Fatalf("first digest error = %v", err)
	}
	reordered := engineContentProposal()
	for i, j := 0, len(reordered.Operations)-1; i < j; i, j = i+1, j-1 {
		reordered.Operations[i], reordered.Operations[j] = reordered.Operations[j], reordered.Operations[i]
	}
	second, err := CanonicalContentDigest(reordered)
	if err != nil {
		t.Fatalf("second digest error = %v", err)
	}
	if first != second || !reflect.DeepEqual(proposal.Operations, original) {
		t.Fatalf("digest/order contract failed: first=%s second=%s mutated=%v", first, second, !reflect.DeepEqual(proposal.Operations, original))
	}
}

func TestValidateContentProposalRejectsAIWithoutProvenanceBinding(t *testing.T) {
	proposal := engineContentProposal()
	proposal.Provenance.Model = ""
	if _, err := ValidateContentProposal(proposal); err == nil {
		t.Fatal("expected missing AI model to be rejected")
	}
	proposal = engineContentProposal()
	proposal.Provenance.PromptDigest = "not-a-digest"
	if _, err := ValidateContentProposal(proposal); err == nil {
		t.Fatal("expected invalid prompt digest to be rejected")
	}
}

func TestValidateContentProposalClassifiesDeleteAsDestructive(t *testing.T) {
	proposal := engineContentProposal()
	proposal.Operations = []ContentOperation{{Kind: ContentKindEvent, Action: ContentActionDelete, ID: "event-wolf-warning"}}
	report, err := ValidateContentProposal(proposal)
	if err != nil {
		t.Fatalf("ValidateContentProposal() error = %v", err)
	}
	if report.Risk != ContentRiskDestructive {
		t.Fatalf("risk=%q, want %q", report.Risk, ContentRiskDestructive)
	}
}

func TestValidateContentProposalRejectsAmbiguousRoomExits(t *testing.T) {
	proposal := engineContentProposal()
	room := proposal.Operations[1].Map.Rooms[0]
	room.Exits = append(room.Exits, ExitSpec{ID: "exit-forest-other", Direction: "북", DestinationRoomID: "room-forest-depth"})
	proposal.Operations[1].Map.Rooms[0] = room
	if _, err := ValidateContentProposal(proposal); err == nil {
		t.Fatal("expected duplicate room direction to be rejected")
	}
}
