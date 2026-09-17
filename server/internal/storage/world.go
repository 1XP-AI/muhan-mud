package storage

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/json"
	"errors"

	"github.com/1XP-Inc/muhan-mud/server/internal/world"
)

var ErrWorldConflict = errors.New("world revision conflict")
var ErrCommandConflict = errors.New("command identity reused with different request")

type WorldSnapshot struct {
	Revision int64
	State    json.RawMessage
}
type WorldReceipt struct {
	Revision int64
	Response json.RawMessage
	Replayed bool
	// Projection is durable, non-public receipt metadata for post-commit
	// delivery. It is hidden from JSON because command responses retain their
	// existing public shape.
	Projection json.RawMessage `json:"-"`
	// ProjectionDelivered is set only after the owning transport has accepted
	// every projected delivery into its connection queues. A replay with a
	// pending projection may safely recover output after a process restart.
	ProjectionDelivered bool `json:"-"`
	// NPCChaseIDs is an ephemeral, process-local projection of the ordered
	// chase moves committed by a fresh movement command. It is deliberately
	// excluded from JSON and is never read from or written to the database;
	// receipt replay therefore always leaves it empty.
	NPCChaseIDs []string `json:"-"`
	// ArrivalTrapEvent is the reducer-owned, first-commit-only room projection
	// for a leader arrival trap. It is process-local receipt metadata and is
	// never serialized into the database/public response.
	ArrivalTrapEvent *world.ArrivalTrapEvent `json:"-"`
	// FollowerArrivalTrapEvents is the ordered projection decoded from a
	// pending private receipt projection. It is hidden from the public response
	// and is empty after the transport acknowledges delivery.
	FollowerArrivalTrapEvents []world.ArrivalTrapEvent `json:"-"`
	// NPCCombatEvents is the reducer-owned, first-commit-only projection of
	// ordinary NPC combat output. It is process-local receipt metadata and is
	// never serialized into the database/public response; replay leaves it
	// empty so a durable retry cannot fan out duplicate combat output.
	NPCCombatEvents []world.NPCCombatEvent `json:"-"`
}

// ReceiptProjectionStore acknowledges a projected receipt after transport
// has queued its output. The operation is idempotent and intentionally does
// not alter the immutable command response or world revision.
type ReceiptProjectionStore interface {
	MarkWorldReceiptProjectionDelivered(context.Context, string, string) error
}

func validState(raw json.RawMessage) bool {
	b := bytes.TrimSpace(raw)
	return len(b) > 0 && b[0] == '{' && json.Valid(b)
}

func (p *Postgres) CreateWorld(ctx context.Context, id string, state json.RawMessage) error {
	if !validState(state) {
		return errors.New("world state must be a JSON object")
	}
	_, err := p.db.ExecContext(ctx, `INSERT INTO mud_go.worlds(id,state) VALUES($1,$2)`, id, string(state))
	return err
}

func (p *Postgres) LoadWorld(ctx context.Context, id string) (WorldSnapshot, error) {
	var s WorldSnapshot
	err := p.db.QueryRowContext(ctx, `SELECT revision,state FROM mud_go.worlds WHERE id=$1`, id).Scan(&s.Revision, &s.State)
	return s, err
}

// ReadWorldReceipt permits retries/recovery without rerunning game transitions.
// Receipt rows are immutable; callers must use the same actor-bound request.
func (p *Postgres) ReadWorldReceipt(ctx context.Context, worldID, commandID string, request json.RawMessage) (WorldReceipt, error) {
	if !json.Valid(request) {
		return WorldReceipt{}, errors.New("invalid command request")
	}
	var epoch int64
	if err := p.db.QueryRowContext(ctx, `SELECT writer_epoch FROM mud_go.worlds WHERE id=$1`, worldID).Scan(&epoch); err != nil {
		return WorldReceipt{}, err
	}
	if err := p.checkWriter(worldID, epoch); err != nil {
		return WorldReceipt{}, err
	}
	var r WorldReceipt
	var stored []byte
	var projection []byte
	err := p.db.QueryRowContext(ctx, `SELECT request_hash,revision,response,projection,projection_delivered FROM mud_go.world_commands WHERE world_id=$1 AND command_id=$2`, worldID, commandID).Scan(&stored, &r.Revision, &r.Response, &projection, &r.ProjectionDelivered)
	if err != nil {
		return WorldReceipt{}, err
	}
	hash := sha256.Sum256(request)
	if !bytes.Equal(stored, hash[:]) {
		return WorldReceipt{}, ErrCommandConflict
	}
	r.Projection = append(json.RawMessage(nil), projection...)
	r.Replayed = true
	return r, nil
}

// CommitWorldCommand is internal-only: an authenticated world loop validates
// gameplay and supplies the complete snapshot and response. request MUST contain
// actor identity and canonical command input, not passwords. Identity compares
// exact request bytes; retry reuses those bytes and command ID unchanged.
// A commit error can mean an unknown outcome: retry the SAME command, never a
// fresh ID. Receipt replay takes precedence over an obsolete expected revision.
func (p *Postgres) CommitWorldCommand(ctx context.Context, worldID, commandID string, request json.RawMessage, expected int64, state, response json.RawMessage) (WorldReceipt, error) {
	return p.commitWorldCommand(ctx, worldID, commandID, request, expected, state, response, nil)
}

// CommitWorldCommandWithProjection atomically stores the canonical response
// and a non-public post-commit projection. Retries return the same projection
// without re-running the reducer or consuming RNG.
func (p *Postgres) CommitWorldCommandWithProjection(ctx context.Context, worldID, commandID string, request json.RawMessage, expected int64, state, response, projection json.RawMessage) (WorldReceipt, error) {
	return p.commitWorldCommand(ctx, worldID, commandID, request, expected, state, response, projection)
}

func (p *Postgres) commitWorldCommand(ctx context.Context, worldID, commandID string, request json.RawMessage, expected int64, state, response, projection json.RawMessage) (WorldReceipt, error) {
	if !validState(state) || !json.Valid(request) || !json.Valid(response) || expected < 0 {
		return WorldReceipt{}, errors.New("invalid world command payload")
	}
	if projection == nil {
		projection = json.RawMessage(`{}`)
	}
	if !json.Valid(projection) {
		return WorldReceipt{}, errors.New("invalid world command projection")
	}
	hash := sha256.Sum256(request)
	tx, err := p.db.BeginTx(ctx, nil)
	if err != nil {
		return WorldReceipt{}, err
	}
	defer tx.Rollback()
	var current, epoch int64
	// Every writer locks the same world before checking/inserting a receipt.
	if err = tx.QueryRowContext(ctx, `SELECT revision,writer_epoch FROM mud_go.worlds WHERE id=$1 FOR UPDATE`, worldID).Scan(&current, &epoch); err != nil {
		return WorldReceipt{}, err
	}
	if err = p.checkWriter(worldID, epoch); err != nil {
		return WorldReceipt{}, err
	}
	var receipt WorldReceipt
	var storedHash []byte
	var storedProjection []byte
	err = tx.QueryRowContext(ctx, `SELECT request_hash,revision,response,projection,projection_delivered FROM mud_go.world_commands WHERE world_id=$1 AND command_id=$2`, worldID, commandID).Scan(&storedHash, &receipt.Revision, &receipt.Response, &storedProjection, &receipt.ProjectionDelivered)
	if err == nil {
		if !bytes.Equal(storedHash, hash[:]) {
			return WorldReceipt{}, ErrCommandConflict
		}
		receipt.Projection = append(json.RawMessage(nil), storedProjection...)
		receipt.Replayed = true
		return receipt, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return WorldReceipt{}, err
	}
	if current != expected {
		return WorldReceipt{}, ErrWorldConflict
	}
	err = tx.QueryRowContext(ctx, `UPDATE mud_go.worlds SET state=$2,revision=revision+1 WHERE id=$1 RETURNING revision`, worldID, string(state)).Scan(&receipt.Revision)
	if err != nil {
		return WorldReceipt{}, err
	}
	err = tx.QueryRowContext(ctx, `INSERT INTO mud_go.world_commands(world_id,command_id,request_hash,revision,response,projection,projection_delivered) VALUES($1,$2,$3,$4,$5,$6,false) RETURNING response,projection,projection_delivered`, worldID, commandID, hash[:], receipt.Revision, string(response), string(projection)).Scan(&receipt.Response, &receipt.Projection, &receipt.ProjectionDelivered)
	if err != nil {
		return WorldReceipt{}, err
	}
	if err = tx.Commit(); err != nil {
		return WorldReceipt{}, err
	}
	return receipt, nil
}

// MarkWorldReceiptProjectionDelivered is an idempotent acknowledgement after
// the transport queue accepts all recipients recorded by the reducer.
func (p *Postgres) MarkWorldReceiptProjectionDelivered(ctx context.Context, worldID, commandID string) error {
	if worldID == "" || commandID == "" {
		return errors.New("invalid projected receipt identity")
	}
	var epoch int64
	if err := p.db.QueryRowContext(ctx, `SELECT writer_epoch FROM mud_go.worlds WHERE id=$1`, worldID).Scan(&epoch); err != nil {
		return err
	}
	if err := p.checkWriter(worldID, epoch); err != nil {
		return err
	}
	_, err := p.db.ExecContext(ctx, `UPDATE mud_go.world_commands SET projection_delivered=true WHERE world_id=$1 AND command_id=$2`, worldID, commandID)
	return err
}
