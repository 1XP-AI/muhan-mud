package engine

import (
	"errors"
	"reflect"
	"testing"
)

func TestApplyContentProposalMaterializesAtomicCatalogAndReceipt(t *testing.T) {
	catalog, err := NewContentCatalog("muhan-01")
	if err != nil {
		t.Fatalf("NewContentCatalog() error = %v", err)
	}
	before := catalog
	proposal := engineContentProposal()
	proposal.BaseRevision = catalog.Revision
	next, receipt, err := catalog.ApplyContentProposal(proposal)
	if err != nil {
		t.Fatalf("ApplyContentProposal() error = %v", err)
	}
	if next.Revision != 1 || receipt.Revision != 1 || receipt.WorldID != "muhan-01" || receipt.ProposalID != "gm-0004" || receipt.ProposalDigest == "" || receipt.CatalogDigest != next.Digest {
		t.Fatalf("unexpected apply result: next=%+v receipt=%+v", next, receipt)
	}
	if len(next.Maps) != 1 || len(next.Rooms) != 2 || len(next.MonsterTemplates) != 1 || len(next.SpawnRules) != 1 || len(next.Scenarios) != 1 || len(next.Events) != 1 {
		t.Fatalf("unexpected materialized counts: maps=%d rooms=%d monsters=%d spawns=%d scenarios=%d events=%d", len(next.Maps), len(next.Rooms), len(next.MonsterTemplates), len(next.SpawnRules), len(next.Scenarios), len(next.Events))
	}
	if err := next.Validate(); err != nil {
		t.Fatalf("materialized catalog validation error = %v", err)
	}
	if !reflect.DeepEqual(catalog, before) || catalog.Revision != 0 || len(catalog.Rooms) != 0 {
		t.Fatal("ApplyContentProposal mutated the source catalog")
	}
}

func TestApplyContentProposalRejectsWorldAndRevisionConflicts(t *testing.T) {
	catalog, err := NewContentCatalog("muhan-01")
	if err != nil {
		t.Fatal(err)
	}
	proposal := engineContentProposal()
	proposal.WorldID = "other-world"
	if _, _, err := catalog.ApplyContentProposal(proposal); !errors.Is(err, ErrContentWorldMismatch) {
		t.Fatalf("world error = %v, want %v", err, ErrContentWorldMismatch)
	}
	proposal = engineContentProposal()
	proposal.BaseRevision = 1
	if _, _, err := catalog.ApplyContentProposal(proposal); !errors.Is(err, ErrContentRevisionConflict) {
		t.Fatalf("revision error = %v, want %v", err, ErrContentRevisionConflict)
	}
}

func TestApplyContentProposalRejectsDanglingDeleteAndKeepsSource(t *testing.T) {
	catalog, err := NewContentCatalog("muhan-01")
	if err != nil {
		t.Fatal(err)
	}
	proposal := engineContentProposal()
	proposal.BaseRevision = catalog.Revision
	materialized, _, err := catalog.ApplyContentProposal(proposal)
	if err != nil {
		t.Fatal(err)
	}
	proposal = ContentProposal{
		SchemaVersion: ContentSchemaVersion,
		WorldID:       "muhan-01",
		BaseRevision:  materialized.Revision,
		ProposalID:    "gm-delete-event",
		Provenance:    ContentProvenance{Source: ContentSourceOperator},
		Operations:    []ContentOperation{{Kind: ContentKindEvent, Action: ContentActionDelete, ID: "event-wolf-warning"}},
	}
	if _, _, err := materialized.ApplyContentProposal(proposal); !errors.Is(err, ErrContentReference) {
		t.Fatalf("delete error = %v, want reference error", err)
	}
	if _, ok := materialized.Events["event-wolf-warning"]; !ok {
		t.Fatal("failed delete changed the source catalog")
	}
}

func TestApplyContentProposalRejectsMapRoomRemoval(t *testing.T) {
	catalog, err := NewContentCatalog("muhan-01")
	if err != nil {
		t.Fatal(err)
	}
	proposal := engineContentProposal()
	proposal.BaseRevision = catalog.Revision
	materialized, _, err := catalog.ApplyContentProposal(proposal)
	if err != nil {
		t.Fatal(err)
	}
	proposal = ContentProposal{
		SchemaVersion: ContentSchemaVersion,
		WorldID:       "muhan-01",
		BaseRevision:  materialized.Revision,
		ProposalID:    "gm-update-map",
		Provenance:    ContentProvenance{Source: ContentSourceOperator},
		Operations: []ContentOperation{{
			Kind: ContentKindMap, Action: ContentActionUpdate, ID: "map-forest",
			Map: &MapSpec{ID: "map-forest", Name: "북쪽 숲", Rooms: []RoomSpec{{ID: "room-forest-edge", MapID: "map-forest", Name: "숲 입구", MinLevel: 1, MaxLevel: 10, Exits: []ExitSpec{{ID: "exit-forest-north", Direction: "북", DestinationRoomID: "room-forest-depth"}}}}},
		}},
	}
	if _, _, err := materialized.ApplyContentProposal(proposal); !errors.Is(err, ErrContentMapMutation) {
		t.Fatalf("map update error = %v, want map mutation error", err)
	}
}
