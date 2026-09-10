package engine

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"unicode"
	"unicode/utf8"
)

// ContentSchemaVersion is the first engine-owned content contract. It is
// deliberately separate from the live world snapshot version: a map or
// scenario revision may be published without rewriting player state.
const ContentSchemaVersion = 1

const (
	maxContentOperations = 256
	maxMapRooms          = 128
	maxRoomExits         = 32
	maxScenarioEvents    = 128
	maxContentTextBytes  = 4096
	maxEventMessageBytes = 2048
	maxSpawnCount        = 1000
	maxEventDelaySeconds = 30 * 24 * 60 * 60
)

// ContentKind names the only entity families an AI GM may author. The engine
// does not accept arbitrary JSON/SQL mutations in place of these types.
type ContentKind string

const (
	ContentKindMap             ContentKind = "map"
	ContentKindRoom            ContentKind = "room"
	ContentKindMonsterTemplate ContentKind = "monster_template"
	ContentKindSpawnRule       ContentKind = "spawn_rule"
	ContentKindScenario        ContentKind = "scenario"
	ContentKindEvent           ContentKind = "event"
)

// ContentAction is intentionally closed. Delete is retained for rollback or
// retirement proposals, but the policy layer must treat it as destructive.
type ContentAction string

const (
	ContentActionCreate ContentAction = "create"
	ContentActionUpdate ContentAction = "update"
	ContentActionDelete ContentAction = "delete"
)

// ContentSource records why a proposal exists. AI proposals must bind a model
// and prompt digest; legacy imports and operator-authored changes do not need
// an AI identity.
type ContentSource string

const (
	ContentSourceAIGM         ContentSource = "ai-gm"
	ContentSourceLegacyImport ContentSource = "legacy-import"
	ContentSourceOperator     ContentSource = "operator"
)

type ContentProvenance struct {
	Source       ContentSource `json:"source"`
	Model        string        `json:"model,omitempty"`
	PromptDigest string        `json:"prompt_digest,omitempty"`
	ParentDigest string        `json:"parent_digest,omitempty"`
	Seed         uint64        `json:"seed"`
}

// ContentProposal is the engine input produced by an operator or AI GM. It is
// a proposal only; validation and deterministic simulation must happen before
// a future PostgreSQL publisher can make it visible to the live world.
type ContentProposal struct {
	SchemaVersion int                `json:"schema_version"`
	WorldID       string             `json:"world_id"`
	BaseRevision  int64              `json:"base_revision"`
	ProposalID    string             `json:"proposal_id"`
	Provenance    ContentProvenance  `json:"provenance"`
	Operations    []ContentOperation `json:"operations"`
}

type ContentOperation struct {
	Kind   ContentKind   `json:"kind"`
	Action ContentAction `json:"action"`
	ID     string        `json:"id"`

	Map             *MapSpec             `json:"map,omitempty"`
	Room            *RoomSpec            `json:"room,omitempty"`
	MonsterTemplate *MonsterTemplateSpec `json:"monster_template,omitempty"`
	SpawnRule       *SpawnRuleSpec       `json:"spawn_rule,omitempty"`
	Scenario        *ScenarioSpec        `json:"scenario,omitempty"`
	Event           *EventSpec           `json:"event,omitempty"`
}

// MapSpec groups rooms into one authored area. Room IDs are global within a
// world so an exit can safely target a room in another map after validation.
type MapSpec struct {
	ID    string     `json:"id"`
	Name  string     `json:"name"`
	Rooms []RoomSpec `json:"rooms"`
}

type RoomSpec struct {
	ID       string     `json:"id"`
	MapID    string     `json:"map_id"`
	Name     string     `json:"name"`
	MinLevel int        `json:"min_level"`
	MaxLevel int        `json:"max_level"`
	Exits    []ExitSpec `json:"exits"`
}

type ExitSpec struct {
	ID                string `json:"id"`
	Direction         string `json:"direction"`
	DestinationRoomID string `json:"destination_room_id"`
	Bidirectional     bool   `json:"bidirectional"`
	Locked            bool   `json:"locked"`
}

type MonsterTemplateSpec struct {
	ID       string   `json:"id"`
	Name     string   `json:"name"`
	MinLevel int      `json:"min_level"`
	MaxLevel int      `json:"max_level"`
	MinHP    int      `json:"min_hp"`
	MaxHP    int      `json:"max_hp"`
	XP       int64    `json:"xp"`
	MinGold  int64    `json:"min_gold"`
	MaxGold  int64    `json:"max_gold"`
	Tags     []string `json:"tags,omitempty"`
}

type SpawnRuleSpec struct {
	ID                string `json:"id"`
	RoomID            string `json:"room_id"`
	MonsterTemplateID string `json:"monster_template_id"`
	MinCount          int    `json:"min_count"`
	MaxCount          int    `json:"max_count"`
	RespawnSeconds    int    `json:"respawn_seconds"`
	LevelOffset       int    `json:"level_offset"`
}

type ScenarioSpec struct {
	ID          string   `json:"id"`
	Name        string   `json:"name"`
	StartRoomID string   `json:"start_room_id"`
	MinLevel    int      `json:"min_level"`
	MaxLevel    int      `json:"max_level"`
	EventIDs    []string `json:"event_ids"`
}

// EventKind is a closed set of effects that can be scheduled by the engine.
// There is intentionally no "run SQL", "execute code", or arbitrary player
// mutation event.
type EventKind string

const (
	EventKindMessage    EventKind = "message"
	EventKindSpawn      EventKind = "spawn"
	EventKindUnlockExit EventKind = "unlock_exit"
	EventKindObjective  EventKind = "objective"
)

type EventSpec struct {
	ID                string    `json:"id"`
	ScenarioID        string    `json:"scenario_id"`
	Kind              EventKind `json:"kind"`
	AtSeconds         int       `json:"at_seconds"`
	RepeatSeconds     int       `json:"repeat_seconds"`
	RoomID            string    `json:"room_id,omitempty"`
	ExitID            string    `json:"exit_id,omitempty"`
	MonsterTemplateID string    `json:"monster_template_id,omitempty"`
	Count             int       `json:"count,omitempty"`
	LevelOffset       int       `json:"level_offset,omitempty"`
	Message           string    `json:"message,omitempty"`
}

type ContentRisk string

const (
	ContentRiskAdditive    ContentRisk = "additive"
	ContentRiskMutating    ContentRisk = "mutating"
	ContentRiskDestructive ContentRisk = "destructive"
)

type ContentValidationReport struct {
	Digest         string      `json:"digest"`
	Risk           ContentRisk `json:"risk"`
	OperationCount int         `json:"operation_count"`
	RoomCount      int         `json:"room_count"`
	EventCount     int         `json:"event_count"`
}

// ValidateContentProposal checks the engine-owned, side-effect-free contract.
// References to entities in an older published revision are intentionally not
// resolved here; the materializer/simulation phase resolves them against the
// base content head. This lets a proposal update one existing room while
// still rejecting malformed local data.
func ValidateContentProposal(proposal ContentProposal) (ContentValidationReport, error) {
	if proposal.SchemaVersion != ContentSchemaVersion {
		return ContentValidationReport{}, fmt.Errorf("unsupported content schema version %d", proposal.SchemaVersion)
	}
	if !validContentID(proposal.WorldID) || !validContentID(proposal.ProposalID) {
		return ContentValidationReport{}, fmt.Errorf("invalid content world or proposal ID")
	}
	if proposal.BaseRevision < 0 {
		return ContentValidationReport{}, fmt.Errorf("content base revision must be non-negative")
	}
	if err := validateProvenance(proposal.Provenance); err != nil {
		return ContentValidationReport{}, err
	}
	if len(proposal.Operations) == 0 || len(proposal.Operations) > maxContentOperations {
		return ContentValidationReport{}, fmt.Errorf("content operation count must be between 1 and %d", maxContentOperations)
	}
	seen := make(map[string]bool, len(proposal.Operations))
	report := ContentValidationReport{OperationCount: len(proposal.Operations), Risk: ContentRiskAdditive}
	for _, operation := range proposal.Operations {
		if !validContentID(operation.ID) {
			return ContentValidationReport{}, fmt.Errorf("invalid %s operation ID %q", operation.Kind, operation.ID)
		}
		key := string(operation.Kind) + ":" + operation.ID
		if seen[key] {
			return ContentValidationReport{}, fmt.Errorf("duplicate content operation %s", key)
		}
		seen[key] = true
		switch operation.Action {
		case ContentActionCreate:
		case ContentActionUpdate:
			if report.Risk == ContentRiskAdditive {
				report.Risk = ContentRiskMutating
			}
		case ContentActionDelete:
			report.Risk = ContentRiskDestructive
		default:
			return ContentValidationReport{}, fmt.Errorf("unsupported content action %q", operation.Action)
		}
		if err := validateOperation(operation, &report); err != nil {
			return ContentValidationReport{}, err
		}
	}
	digest, err := CanonicalContentDigest(proposal)
	if err != nil {
		return ContentValidationReport{}, err
	}
	report.Digest = digest
	return report, nil
}

// CanonicalContentDigest binds retries and publish records to one stable
// representation. Operation order is presentation-only; entity arrays retain
// authored order because exit and event ordering can affect the source-facing
// projection. The input proposal is never mutated.
func CanonicalContentDigest(proposal ContentProposal) (string, error) {
	canonical, err := canonicalContentProposal(proposal)
	if err != nil {
		return "", err
	}
	raw, err := json.Marshal(canonical)
	if err != nil {
		return "", err
	}
	digest := sha256.Sum256(raw)
	return hex.EncodeToString(digest[:]), nil
}

func canonicalContentProposal(proposal ContentProposal) (ContentProposal, error) {
	if proposal.SchemaVersion != ContentSchemaVersion {
		return ContentProposal{}, fmt.Errorf("unsupported content schema version %d", proposal.SchemaVersion)
	}
	out := proposal
	out.Operations = make([]ContentOperation, len(proposal.Operations))
	for i, operation := range proposal.Operations {
		out.Operations[i] = cloneContentOperation(operation)
	}
	sort.SliceStable(out.Operations, func(i, j int) bool {
		if out.Operations[i].Kind != out.Operations[j].Kind {
			return out.Operations[i].Kind < out.Operations[j].Kind
		}
		return out.Operations[i].ID < out.Operations[j].ID
	})
	return out, nil
}

func cloneContentOperation(in ContentOperation) ContentOperation {
	out := in
	if in.Map != nil {
		value := *in.Map
		value.Rooms = append([]RoomSpec(nil), in.Map.Rooms...)
		for i := range value.Rooms {
			value.Rooms[i].Exits = append([]ExitSpec(nil), value.Rooms[i].Exits...)
		}
		out.Map = &value
	}
	if in.Room != nil {
		value := *in.Room
		value.Exits = append([]ExitSpec(nil), in.Room.Exits...)
		out.Room = &value
	}
	if in.MonsterTemplate != nil {
		value := *in.MonsterTemplate
		value.Tags = append([]string(nil), in.MonsterTemplate.Tags...)
		out.MonsterTemplate = &value
	}
	if in.SpawnRule != nil {
		value := *in.SpawnRule
		out.SpawnRule = &value
	}
	if in.Scenario != nil {
		value := *in.Scenario
		value.EventIDs = append([]string(nil), in.Scenario.EventIDs...)
		out.Scenario = &value
	}
	if in.Event != nil {
		value := *in.Event
		out.Event = &value
	}
	return out
}

func validateProvenance(provenance ContentProvenance) error {
	switch provenance.Source {
	case ContentSourceAIGM:
		if strings.TrimSpace(provenance.Model) == "" || !validDigest(provenance.PromptDigest) {
			return fmt.Errorf("AI GM provenance requires model and prompt digest")
		}
	case ContentSourceLegacyImport, ContentSourceOperator:
	default:
		return fmt.Errorf("unsupported content provenance source %q", provenance.Source)
	}
	if provenance.Model != "" && !validContentText(provenance.Model, 256, false) {
		return fmt.Errorf("invalid content provenance model")
	}
	if provenance.ParentDigest != "" && !validDigest(provenance.ParentDigest) {
		return fmt.Errorf("invalid content parent digest")
	}
	return nil
}

func validateOperation(operation ContentOperation, report *ContentValidationReport) error {
	switch operation.Kind {
	case ContentKindMap, ContentKindRoom, ContentKindMonsterTemplate, ContentKindSpawnRule, ContentKindScenario, ContentKindEvent:
	default:
		return fmt.Errorf("unsupported content kind %q", operation.Kind)
	}
	if operation.Action == ContentActionDelete {
		if operation.Map != nil || operation.Room != nil || operation.MonsterTemplate != nil || operation.SpawnRule != nil || operation.Scenario != nil || operation.Event != nil {
			return fmt.Errorf("delete operation %s must not carry a replacement payload", operation.ID)
		}
		return nil
	}
	if !exactlyOnePayload(operation) {
		return fmt.Errorf("operation %s must carry exactly one typed payload", operation.ID)
	}
	switch operation.Kind {
	case ContentKindMap:
		if operation.Map == nil {
			return fmt.Errorf("map operation %s has wrong payload", operation.ID)
		}
		if err := validateMap(*operation.Map, operation.ID, report); err != nil {
			return err
		}
	case ContentKindRoom:
		if operation.Room == nil {
			return fmt.Errorf("room operation %s has wrong payload", operation.ID)
		}
		if err := validateRoom(*operation.Room, operation.ID); err != nil {
			return err
		}
		report.RoomCount++
	case ContentKindMonsterTemplate:
		if operation.MonsterTemplate == nil {
			return fmt.Errorf("monster operation %s has wrong payload", operation.ID)
		}
		if err := validateMonsterTemplate(*operation.MonsterTemplate, operation.ID); err != nil {
			return err
		}
	case ContentKindSpawnRule:
		if operation.SpawnRule == nil {
			return fmt.Errorf("spawn rule operation %s has wrong payload", operation.ID)
		}
		if err := validateSpawnRule(*operation.SpawnRule, operation.ID); err != nil {
			return err
		}
	case ContentKindScenario:
		if operation.Scenario == nil {
			return fmt.Errorf("scenario operation %s has wrong payload", operation.ID)
		}
		if err := validateScenario(*operation.Scenario, operation.ID); err != nil {
			return err
		}
	case ContentKindEvent:
		if operation.Event == nil {
			return fmt.Errorf("event operation %s has wrong payload", operation.ID)
		}
		if err := validateEvent(*operation.Event, operation.ID); err != nil {
			return err
		}
		report.EventCount++
	default:
		return fmt.Errorf("unsupported content kind %q", operation.Kind)
	}
	return nil
}

func exactlyOnePayload(operation ContentOperation) bool {
	count := 0
	if operation.Map != nil {
		count++
	}
	if operation.Room != nil {
		count++
	}
	if operation.MonsterTemplate != nil {
		count++
	}
	if operation.SpawnRule != nil {
		count++
	}
	if operation.Scenario != nil {
		count++
	}
	if operation.Event != nil {
		count++
	}
	return count == 1
}

func validateMap(value MapSpec, operationID string, report *ContentValidationReport) error {
	if value.ID != operationID || !validContentID(value.ID) || !validContentText(value.Name, 256, true) {
		return fmt.Errorf("invalid map %s", operationID)
	}
	if len(value.Rooms) == 0 || len(value.Rooms) > maxMapRooms {
		return fmt.Errorf("map %s room count must be between 1 and %d", operationID, maxMapRooms)
	}
	seen := make(map[string]bool, len(value.Rooms))
	for _, room := range value.Rooms {
		if seen[room.ID] {
			return fmt.Errorf("map %s has duplicate room %s", operationID, room.ID)
		}
		seen[room.ID] = true
		if err := validateRoom(room, room.ID); err != nil {
			return err
		}
		if room.MapID != value.ID {
			return fmt.Errorf("room %s does not belong to map %s", room.ID, value.ID)
		}
		report.RoomCount++
	}
	return nil
}

func validateRoom(value RoomSpec, operationID string) error {
	if value.ID != operationID || !validContentID(value.ID) || !validContentID(value.MapID) || !validContentText(value.Name, 256, true) || !validLevelRange(value.MinLevel, value.MaxLevel) {
		return fmt.Errorf("invalid room %s", operationID)
	}
	if len(value.Exits) > maxRoomExits {
		return fmt.Errorf("room %s has too many exits", operationID)
	}
	seenIDs := make(map[string]bool, len(value.Exits))
	seenDirections := make(map[string]bool, len(value.Exits))
	for _, exit := range value.Exits {
		if !validContentID(exit.ID) || !validContentText(exit.Direction, 64, true) || !validContentID(exit.DestinationRoomID) {
			return fmt.Errorf("invalid exit in room %s", operationID)
		}
		if seenIDs[exit.ID] || seenDirections[exit.Direction] {
			return fmt.Errorf("duplicate exit in room %s", operationID)
		}
		seenIDs[exit.ID] = true
		seenDirections[exit.Direction] = true
	}
	return nil
}

func validateMonsterTemplate(value MonsterTemplateSpec, operationID string) error {
	if value.ID != operationID || !validContentID(value.ID) || !validContentText(value.Name, 256, true) || !validLevelRange(value.MinLevel, value.MaxLevel) || value.MinHP < 1 || value.MaxHP < value.MinHP || value.MaxHP > 32767 || value.XP < 0 || value.MinGold < 0 || value.MaxGold < value.MinGold {
		return fmt.Errorf("invalid monster template %s", operationID)
	}
	if value.MaxGold > 300000000 {
		return fmt.Errorf("monster template %s gold exceeds world limit", operationID)
	}
	seen := map[string]bool{}
	for _, tag := range value.Tags {
		if !validContentID(tag) || seen[tag] {
			return fmt.Errorf("invalid or duplicate monster tag in %s", operationID)
		}
		seen[tag] = true
	}
	return nil
}

func validateSpawnRule(value SpawnRuleSpec, operationID string) error {
	if value.ID != operationID || !validContentID(value.ID) || !validContentID(value.RoomID) || !validContentID(value.MonsterTemplateID) || value.MinCount < 0 || value.MaxCount < value.MinCount || value.MaxCount > maxSpawnCount || value.RespawnSeconds < 1 || value.RespawnSeconds > maxEventDelaySeconds || value.LevelOffset < -100 || value.LevelOffset > 100 {
		return fmt.Errorf("invalid spawn rule %s", operationID)
	}
	return nil
}

func validateScenario(value ScenarioSpec, operationID string) error {
	if value.ID != operationID || !validContentID(value.ID) || !validContentText(value.Name, 256, true) || !validContentID(value.StartRoomID) || !validLevelRange(value.MinLevel, value.MaxLevel) || len(value.EventIDs) == 0 || len(value.EventIDs) > maxScenarioEvents {
		return fmt.Errorf("invalid scenario %s", operationID)
	}
	seen := map[string]bool{}
	for _, eventID := range value.EventIDs {
		if !validContentID(eventID) || seen[eventID] {
			return fmt.Errorf("invalid or duplicate scenario event in %s", operationID)
		}
		seen[eventID] = true
	}
	return nil
}

func validateEvent(value EventSpec, operationID string) error {
	if value.ID != operationID || !validContentID(value.ID) || !validContentID(value.ScenarioID) || value.AtSeconds < 0 || value.AtSeconds > maxEventDelaySeconds || value.RepeatSeconds < 0 || value.RepeatSeconds > maxEventDelaySeconds || value.LevelOffset < -100 || value.LevelOffset > 100 {
		return fmt.Errorf("invalid event %s", operationID)
	}
	switch value.Kind {
	case EventKindMessage:
		if !validContentText(value.Message, maxEventMessageBytes, true) || value.RoomID == "" || !validContentID(value.RoomID) {
			return fmt.Errorf("message event %s requires room and message", operationID)
		}
	case EventKindSpawn:
		if !validContentID(value.RoomID) || !validContentID(value.MonsterTemplateID) || value.Count < 1 || value.Count > maxSpawnCount {
			return fmt.Errorf("spawn event %s requires bounded room, template, and count", operationID)
		}
	case EventKindUnlockExit:
		if !validContentID(value.RoomID) || !validContentID(value.ExitID) {
			return fmt.Errorf("unlock event %s requires room and exit", operationID)
		}
	case EventKindObjective:
		if !validContentText(value.Message, maxEventMessageBytes, true) {
			return fmt.Errorf("objective event %s requires bounded text", operationID)
		}
	default:
		return fmt.Errorf("unsupported event kind %q", value.Kind)
	}
	return nil
}

func validLevelRange(min, max int) bool { return min >= 0 && max >= min && max <= 255 }

func validContentID(value string) bool {
	if len(value) == 0 || len(value) > 64 || value[0] == '-' || value[len(value)-1] == '-' {
		return false
	}
	for i, r := range value {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '-' || r == '_' || r == '.' {
			continue
		}
		if i == 0 || unicode.IsUpper(r) {
			return false
		}
		return false
	}
	return true
}

func validContentText(value string, maxBytes int, required bool) bool {
	if !utf8.ValidString(value) || len(value) > maxBytes || len(value) > maxContentTextBytes || (required && strings.TrimSpace(value) == "") {
		return false
	}
	for _, r := range value {
		if unicode.IsControl(r) || r == '\u2028' || r == '\u2029' {
			return false
		}
	}
	return true
}

func validDigest(value string) bool {
	if len(value) != sha256.Size*2 {
		return false
	}
	_, err := hex.DecodeString(value)
	return err == nil
}
