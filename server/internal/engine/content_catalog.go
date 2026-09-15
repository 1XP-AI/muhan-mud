package engine

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
)

var (
	ErrContentWorldMismatch    = errors.New("content proposal belongs to another world")
	ErrContentRevisionConflict = errors.New("content base revision conflict")
	ErrContentEntityExists     = errors.New("content entity already exists")
	ErrContentEntityMissing    = errors.New("content entity is missing")
	ErrContentReference        = errors.New("content reference is invalid")
	ErrContentMapMutation      = errors.New("content map mutation is not allowed")
)

// ContentCatalog is the in-memory materialized form of one published content
// head. It is intentionally independent from the live world snapshot: a
// catalog update can be simulated and swapped without moving a player.
type ContentCatalog struct {
	SchemaVersion    int                            `json:"schema_version"`
	WorldID          string                         `json:"world_id"`
	Revision         int64                          `json:"revision"`
	Digest           string                         `json:"digest"`
	Maps             map[string]MapSpec             `json:"maps"`
	Rooms            map[string]RoomSpec            `json:"rooms"`
	MonsterTemplates map[string]MonsterTemplateSpec `json:"monster_templates"`
	SpawnRules       map[string]SpawnRuleSpec       `json:"spawn_rules"`
	Scenarios        map[string]ScenarioSpec        `json:"scenarios"`
	Events           map[string]EventSpec           `json:"events"`
}

// ContentApplyReceipt is the deterministic result of materialization. The
// storage publisher will persist this receipt together with the immutable
// proposal and use ProposalDigest to detect an ID reused with other bytes.
type ContentApplyReceipt struct {
	WorldID        string      `json:"world_id"`
	ProposalID     string      `json:"proposal_id"`
	ProposalDigest string      `json:"proposal_digest"`
	Revision       int64       `json:"revision"`
	CatalogDigest  string      `json:"catalog_digest"`
	Risk           ContentRisk `json:"risk"`
}

// NewContentCatalog creates an empty, revision-zero head for a world. It does
// not seed legacy resources or write PostgreSQL.
func NewContentCatalog(worldID string) (ContentCatalog, error) {
	if !validContentID(worldID) {
		return ContentCatalog{}, fmt.Errorf("invalid content world ID")
	}
	catalog := ContentCatalog{
		SchemaVersion:    ContentSchemaVersion,
		WorldID:          worldID,
		Maps:             map[string]MapSpec{},
		Rooms:            map[string]RoomSpec{},
		MonsterTemplates: map[string]MonsterTemplateSpec{},
		SpawnRules:       map[string]SpawnRuleSpec{},
		Scenarios:        map[string]ScenarioSpec{},
		Events:           map[string]EventSpec{},
	}
	digest, err := contentCatalogDigest(catalog)
	if err != nil {
		return ContentCatalog{}, err
	}
	catalog.Digest = digest
	return catalog, nil
}

// Validate checks the complete materialized graph. Unlike proposal
// validation, all references must resolve because the catalog is ready for a
// runtime consumer.
func (c ContentCatalog) Validate() error {
	if c.SchemaVersion != ContentSchemaVersion || !validContentID(c.WorldID) || c.Revision < 0 || c.Maps == nil || c.Rooms == nil || c.MonsterTemplates == nil || c.SpawnRules == nil || c.Scenarios == nil || c.Events == nil {
		return fmt.Errorf("invalid content catalog envelope")
	}
	if c.Digest != "" {
		if !validDigest(c.Digest) {
			return fmt.Errorf("invalid content catalog digest")
		}
		expected, err := contentCatalogDigest(c)
		if err != nil {
			return err
		}
		if expected != c.Digest {
			return fmt.Errorf("content catalog digest mismatch")
		}
	}
	roomMap := make(map[string]string, len(c.Rooms))
	for id, value := range c.Maps {
		if err := validateMap(value, id, &ContentValidationReport{}); err != nil {
			return err
		}
		for _, room := range value.Rooms {
			if prior, exists := roomMap[room.ID]; exists && prior != id {
				return fmt.Errorf("%w: room %s belongs to maps %s and %s", ErrContentReference, room.ID, prior, id)
			}
			roomMap[room.ID] = id
			indexed, exists := c.Rooms[room.ID]
			if !exists || !reflect.DeepEqual(indexed, room) {
				return fmt.Errorf("%w: map room %s", ErrContentReference, room.ID)
			}
		}
	}
	if len(roomMap) != len(c.Rooms) {
		return fmt.Errorf("%w: unlisted room", ErrContentReference)
	}
	for id, room := range c.Rooms {
		if err := validateRoom(room, id); err != nil {
			return err
		}
		if _, exists := c.Maps[room.MapID]; !exists {
			return fmt.Errorf("%w: room %s map %s", ErrContentReference, id, room.MapID)
		}
		for _, exit := range room.Exits {
			if _, exists := c.Rooms[exit.DestinationRoomID]; !exists {
				return fmt.Errorf("%w: room %s exit %s destination", ErrContentReference, id, exit.ID)
			}
		}
	}
	for id, value := range c.MonsterTemplates {
		if err := validateMonsterTemplate(value, id); err != nil {
			return err
		}
	}
	for id, value := range c.SpawnRules {
		if err := validateSpawnRule(value, id); err != nil {
			return err
		}
		if _, exists := c.Rooms[value.RoomID]; !exists {
			return fmt.Errorf("%w: spawn rule %s room", ErrContentReference, id)
		}
		if _, exists := c.MonsterTemplates[value.MonsterTemplateID]; !exists {
			return fmt.Errorf("%w: spawn rule %s template", ErrContentReference, id)
		}
	}
	eventOwners := make(map[string]string, len(c.Events))
	for id, value := range c.Scenarios {
		if err := validateScenario(value, id); err != nil {
			return err
		}
		if _, exists := c.Rooms[value.StartRoomID]; !exists {
			return fmt.Errorf("%w: scenario %s start room", ErrContentReference, id)
		}
		for _, eventID := range value.EventIDs {
			event, exists := c.Events[eventID]
			if !exists || event.ScenarioID != id {
				return fmt.Errorf("%w: scenario %s event %s", ErrContentReference, id, eventID)
			}
			if prior, exists := eventOwners[eventID]; exists && prior != id {
				return fmt.Errorf("%w: event %s belongs to scenarios %s and %s", ErrContentReference, eventID, prior, id)
			}
			eventOwners[eventID] = id
		}
	}
	for id, value := range c.Events {
		if err := validateEvent(value, id); err != nil {
			return err
		}
		if _, exists := c.Scenarios[value.ScenarioID]; !exists {
			return fmt.Errorf("%w: event %s scenario", ErrContentReference, id)
		}
		if owner, listed := eventOwners[id]; !listed || owner != value.ScenarioID {
			return fmt.Errorf("%w: event %s is not listed by its scenario", ErrContentReference, id)
		}
		if value.RoomID != "" {
			room, exists := c.Rooms[value.RoomID]
			if !exists {
				return fmt.Errorf("%w: event %s room", ErrContentReference, id)
			}
			if value.Kind == EventKindUnlockExit {
				found := false
				for _, exit := range room.Exits {
					if exit.ID == value.ExitID {
						found = true
						break
					}
				}
				if !found {
					return fmt.Errorf("%w: event %s exit", ErrContentReference, id)
				}
			}
		}
		if value.MonsterTemplateID != "" {
			if _, exists := c.MonsterTemplates[value.MonsterTemplateID]; !exists {
				return fmt.Errorf("%w: event %s template", ErrContentReference, id)
			}
		}
	}
	return nil
}

// ApplyContentProposal materializes a proposal against an exact content head.
// It returns a zero catalog on every failure; the receiver is never mutated.
// Persistence, idempotent receipt lookup, and writer fencing remain storage
// responsibilities.
func (c ContentCatalog) ApplyContentProposal(proposal ContentProposal) (ContentCatalog, ContentApplyReceipt, error) {
	if err := c.Validate(); err != nil {
		return ContentCatalog{}, ContentApplyReceipt{}, err
	}
	report, err := ValidateContentProposal(proposal)
	if err != nil {
		return ContentCatalog{}, ContentApplyReceipt{}, err
	}
	if proposal.WorldID != c.WorldID {
		return ContentCatalog{}, ContentApplyReceipt{}, ErrContentWorldMismatch
	}
	if proposal.BaseRevision != c.Revision {
		return ContentCatalog{}, ContentApplyReceipt{}, fmt.Errorf("%w: current=%d base=%d", ErrContentRevisionConflict, c.Revision, proposal.BaseRevision)
	}
	canonical, err := canonicalContentProposal(proposal)
	if err != nil {
		return ContentCatalog{}, ContentApplyReceipt{}, err
	}
	next := c.clone()
	// Mutations invalidate the source digest until the complete candidate has
	// passed graph validation and receives its new head digest below.
	next.Digest = ""
	for _, operation := range canonical.Operations {
		if err := next.applyContentOperation(operation); err != nil {
			return ContentCatalog{}, ContentApplyReceipt{}, err
		}
	}
	if err := next.Validate(); err != nil {
		return ContentCatalog{}, ContentApplyReceipt{}, err
	}
	next.Revision++
	next.Digest, err = contentCatalogDigest(next)
	if err != nil {
		return ContentCatalog{}, ContentApplyReceipt{}, err
	}
	return next, ContentApplyReceipt{
		WorldID: next.WorldID, ProposalID: proposal.ProposalID,
		ProposalDigest: report.Digest, Revision: next.Revision,
		CatalogDigest: next.Digest, Risk: report.Risk,
	}, nil
}

func (c ContentCatalog) applyContentOperation(operation ContentOperation) error {
	if operation.Action == ContentActionDelete {
		return c.deleteContentOperation(operation)
	}
	switch operation.Kind {
	case ContentKindMap:
		return c.applyMap(operation.Action, *operation.Map)
	case ContentKindRoom:
		return c.applyRoom(operation.Action, *operation.Room)
	case ContentKindMonsterTemplate:
		return applyEntity(operation.Action, operation.ID, c.MonsterTemplates, *operation.MonsterTemplate)
	case ContentKindSpawnRule:
		return applyEntity(operation.Action, operation.ID, c.SpawnRules, *operation.SpawnRule)
	case ContentKindScenario:
		return applyEntity(operation.Action, operation.ID, c.Scenarios, *operation.Scenario)
	case ContentKindEvent:
		return applyEntity(operation.Action, operation.ID, c.Events, *operation.Event)
	default:
		return fmt.Errorf("unsupported content kind %q", operation.Kind)
	}
}

func applyEntity[T any](action ContentAction, id string, entities map[string]T, value T) error {
	_, exists := entities[id]
	switch action {
	case ContentActionCreate:
		if exists {
			return fmt.Errorf("%w: %s", ErrContentEntityExists, id)
		}
	case ContentActionUpdate:
		if !exists {
			return fmt.Errorf("%w: %s", ErrContentEntityMissing, id)
		}
	default:
		return fmt.Errorf("unsupported content action %q", action)
	}
	entities[id] = value
	return nil
}

func (c ContentCatalog) applyMap(action ContentAction, value MapSpec) error {
	old, exists := c.Maps[value.ID]
	switch action {
	case ContentActionCreate:
		if exists {
			return fmt.Errorf("%w: %s", ErrContentEntityExists, value.ID)
		}
		for _, room := range value.Rooms {
			if _, occupied := c.Rooms[room.ID]; occupied {
				return fmt.Errorf("%w: room %s", ErrContentEntityExists, room.ID)
			}
		}
	case ContentActionUpdate:
		if !exists {
			return fmt.Errorf("%w: %s", ErrContentEntityMissing, value.ID)
		}
		oldIDs := make(map[string]bool, len(old.Rooms))
		for _, room := range old.Rooms {
			oldIDs[room.ID] = true
		}
		newIDs := make(map[string]bool, len(value.Rooms))
		for _, room := range value.Rooms {
			newIDs[room.ID] = true
			if room.MapID != value.ID {
				return fmt.Errorf("%w: room %s map changed", ErrContentMapMutation, room.ID)
			}
			if owner, occupied := c.Rooms[room.ID]; occupied && !oldIDs[room.ID] {
				return fmt.Errorf("%w: room %s already belongs to another map", ErrContentEntityExists, owner.ID)
			}
		}
		for roomID := range oldIDs {
			if !newIDs[roomID] {
				return fmt.Errorf("%w: map update cannot remove room %s", ErrContentMapMutation, roomID)
			}
		}
	default:
		return fmt.Errorf("unsupported content action %q", action)
	}
	c.Maps[value.ID] = cloneMapSpec(value)
	for _, room := range value.Rooms {
		c.Rooms[room.ID] = cloneRoomSpec(room)
	}
	return nil
}

func (c ContentCatalog) applyRoom(action ContentAction, value RoomSpec) error {
	if _, exists := c.Maps[value.MapID]; !exists {
		return fmt.Errorf("%w: room %s map %s", ErrContentReference, value.ID, value.MapID)
	}
	old, exists := c.Rooms[value.ID]
	if exists && action == ContentActionCreate {
		return fmt.Errorf("%w: %s", ErrContentEntityExists, value.ID)
	}
	if !exists && action == ContentActionUpdate {
		return fmt.Errorf("%w: %s", ErrContentEntityMissing, value.ID)
	}
	if exists && old.MapID != value.MapID {
		return fmt.Errorf("%w: room cannot move between maps", ErrContentMapMutation)
	}
	if action != ContentActionCreate && action != ContentActionUpdate {
		return fmt.Errorf("unsupported content action %q", action)
	}
	c.Rooms[value.ID] = cloneRoomSpec(value)
	rooms := c.Maps[value.MapID].Rooms
	found := false
	for i := range rooms {
		if rooms[i].ID == value.ID {
			rooms[i] = cloneRoomSpec(value)
			found = true
			break
		}
	}
	if !found {
		rooms = append(rooms, cloneRoomSpec(value))
	}
	mapValue := c.Maps[value.MapID]
	mapValue.Rooms = rooms
	c.Maps[value.MapID] = mapValue
	return nil
}

func (c ContentCatalog) deleteContentOperation(operation ContentOperation) error {
	switch operation.Kind {
	case ContentKindMap:
		value, exists := c.Maps[operation.ID]
		if !exists {
			return fmt.Errorf("%w: %s", ErrContentEntityMissing, operation.ID)
		}
		if len(value.Rooms) != 0 {
			return fmt.Errorf("%w: map %s still owns rooms", ErrContentReference, operation.ID)
		}
		delete(c.Maps, operation.ID)
	case ContentKindRoom:
		value, exists := c.Rooms[operation.ID]
		if !exists {
			return fmt.Errorf("%w: %s", ErrContentEntityMissing, operation.ID)
		}
		for _, spawn := range c.SpawnRules {
			if spawn.RoomID == operation.ID {
				return fmt.Errorf("%w: room %s spawn rule", ErrContentReference, operation.ID)
			}
		}
		for _, event := range c.Events {
			if event.RoomID == operation.ID {
				return fmt.Errorf("%w: room %s event", ErrContentReference, operation.ID)
			}
		}
		for _, room := range c.Rooms {
			for _, exit := range room.Exits {
				if exit.DestinationRoomID == operation.ID {
					return fmt.Errorf("%w: room %s exit", ErrContentReference, operation.ID)
				}
			}
		}
		mapValue := c.Maps[value.MapID]
		for i, room := range mapValue.Rooms {
			if room.ID == operation.ID {
				mapValue.Rooms = append(mapValue.Rooms[:i], mapValue.Rooms[i+1:]...)
				break
			}
		}
		c.Maps[value.MapID] = mapValue
		delete(c.Rooms, operation.ID)
	case ContentKindMonsterTemplate:
		if _, exists := c.MonsterTemplates[operation.ID]; !exists {
			return fmt.Errorf("%w: %s", ErrContentEntityMissing, operation.ID)
		}
		for _, spawn := range c.SpawnRules {
			if spawn.MonsterTemplateID == operation.ID {
				return fmt.Errorf("%w: template %s spawn rule", ErrContentReference, operation.ID)
			}
		}
		for _, event := range c.Events {
			if event.MonsterTemplateID == operation.ID {
				return fmt.Errorf("%w: template %s event", ErrContentReference, operation.ID)
			}
		}
		delete(c.MonsterTemplates, operation.ID)
	case ContentKindSpawnRule:
		if _, exists := c.SpawnRules[operation.ID]; !exists {
			return fmt.Errorf("%w: %s", ErrContentEntityMissing, operation.ID)
		}
		delete(c.SpawnRules, operation.ID)
	case ContentKindScenario:
		value, exists := c.Scenarios[operation.ID]
		if !exists {
			return fmt.Errorf("%w: %s", ErrContentEntityMissing, operation.ID)
		}
		if len(value.EventIDs) != 0 {
			return fmt.Errorf("%w: scenario %s still owns events", ErrContentReference, operation.ID)
		}
		delete(c.Scenarios, operation.ID)
	case ContentKindEvent:
		if _, exists := c.Events[operation.ID]; !exists {
			return fmt.Errorf("%w: %s", ErrContentEntityMissing, operation.ID)
		}
		for id, scenario := range c.Scenarios {
			for _, eventID := range scenario.EventIDs {
				if eventID == operation.ID {
					return fmt.Errorf("%w: event %s scenario %s", ErrContentReference, operation.ID, id)
				}
			}
		}
		delete(c.Events, operation.ID)
	default:
		return fmt.Errorf("unsupported content kind %q", operation.Kind)
	}
	return nil
}

func (c ContentCatalog) clone() ContentCatalog {
	out := c
	out.Maps = make(map[string]MapSpec, len(c.Maps))
	for id, value := range c.Maps {
		out.Maps[id] = cloneMapSpec(value)
	}
	out.Rooms = make(map[string]RoomSpec, len(c.Rooms))
	for id, value := range c.Rooms {
		out.Rooms[id] = cloneRoomSpec(value)
	}
	out.MonsterTemplates = make(map[string]MonsterTemplateSpec, len(c.MonsterTemplates))
	for id, value := range c.MonsterTemplates {
		value.Tags = append([]string(nil), value.Tags...)
		out.MonsterTemplates[id] = value
	}
	out.SpawnRules = make(map[string]SpawnRuleSpec, len(c.SpawnRules))
	for id, value := range c.SpawnRules {
		out.SpawnRules[id] = value
	}
	out.Scenarios = make(map[string]ScenarioSpec, len(c.Scenarios))
	for id, value := range c.Scenarios {
		value.EventIDs = append([]string(nil), value.EventIDs...)
		out.Scenarios[id] = value
	}
	out.Events = make(map[string]EventSpec, len(c.Events))
	for id, value := range c.Events {
		out.Events[id] = value
	}
	return out
}

func cloneMapSpec(value MapSpec) MapSpec {
	rooms := append([]RoomSpec(nil), value.Rooms...)
	value.Rooms = make([]RoomSpec, len(rooms))
	for i, room := range rooms {
		value.Rooms[i] = cloneRoomSpec(room)
	}
	return value
}

func cloneRoomSpec(value RoomSpec) RoomSpec {
	value.Exits = append([]ExitSpec(nil), value.Exits...)
	return value
}

func contentCatalogDigest(catalog ContentCatalog) (string, error) {
	copy := catalog
	copy.Digest = ""
	return digestJSON(copy)
}

func digestJSON(value any) (string, error) {
	raw, err := json.Marshal(value)
	if err != nil {
		return "", err
	}
	digest := sha256.Sum256(raw)
	return hex.EncodeToString(digest[:]), nil
}
