package world

// BankSnapshotV1 is the Go reader for the portable kind-8 bank artifact.  It
// deliberately reuses the established ObjectGraphV1 codec but adds the bank
// contract that the graph contains exactly one detached root.  This is an
// offline evidence boundary: decoding it never grants a player or account any
// gameplay authority and never writes PostgreSQL.

import (
	"bytes"
	"crypto/sha256"
	"errors"
	"fmt"
)

const bankSnapshotArtifactLimit = 4 * 1024 * 1024

var (
	ErrBankSnapshotMalformed      = errors.New("malformed bank snapshot CDTO")
	ErrBankSnapshotGraphInvalid   = errors.New("invalid bank snapshot object graph")
	ErrBankSnapshotDigestMismatch = errors.New("bank snapshot CDTO digest mismatch")
	ErrBankSnapshotNonCanonical   = errors.New("non-canonical bank snapshot CDTO")
	ErrBankSnapshotSizeLimit      = errors.New("bank snapshot exceeds size limit")
)

// BankSnapshotV1 owns one detached legacy object graph.  Root and node values
// are copies; callers may safely mutate their result without changing source
// evidence or a later decode.
type BankSnapshotV1 struct {
	Root PlayerSnapshotObjectGraphV1
}

// BankSnapshotV1Inspection retains immutable source evidence for an operator
// import/recovery pipeline.  The raw bytes are never included in gameplay
// receipts or terminal output.
type BankSnapshotV1Inspection struct {
	Snapshot BankSnapshotV1
	Source   []byte
	SHA256   [sha256.Size]byte
}

func (i BankSnapshotV1Inspection) Clone() BankSnapshotV1Inspection {
	i.Source = append([]byte(nil), i.Source...)
	i.Snapshot.Root = clonePlayerSnapshotObjectGraph(i.Snapshot.Root)
	return i
}

// DecodeBankSnapshotV1 validates a complete canonical kind-8 envelope.  The
// embedded graph is the same ObjectGraphV1 wire format used by C and Rust;
// only a single root is admitted for bank semantics.
func DecodeBankSnapshotV1(raw []byte) (BankSnapshotV1, error) {
	if len(raw) > bankSnapshotArtifactLimit {
		return BankSnapshotV1{}, ErrBankSnapshotSizeLimit
	}
	record, err := decodePlayerSnapshotRecord(raw)
	if err != nil {
		return BankSnapshotV1{}, fmt.Errorf("%w: %v", ErrBankSnapshotMalformed, err)
	}
	if record.kind != bankSnapshotKind || len(record.fields) != 1 {
		return BankSnapshotV1{}, fmt.Errorf("%w: kind or field count", ErrBankSnapshotMalformed)
	}
	field := record.fields[0]
	if field.id != 1 || field.tag != playerSnapshotTypeBytes || len(field.value) == 0 {
		return BankSnapshotV1{}, fmt.Errorf("%w: graph field", ErrBankSnapshotMalformed)
	}
	graph, err := decodePlayerSnapshotObjectGraph(field.value)
	if err != nil {
		return BankSnapshotV1{}, fmt.Errorf("%w: %v", ErrBankSnapshotGraphInvalid, err)
	}
	if !bankSnapshotOneRoot(graph) {
		return BankSnapshotV1{}, ErrBankSnapshotGraphInvalid
	}
	canonical, err := encodeBankSnapshotV1(BankSnapshotV1{Root: graph})
	if err != nil {
		return BankSnapshotV1{}, err
	}
	if !bytes.Equal(canonical, raw) {
		return BankSnapshotV1{}, ErrBankSnapshotNonCanonical
	}
	return BankSnapshotV1{Root: graph}, nil
}

// InspectBankSnapshotV1 adds a detached source digest to DecodeBankSnapshotV1.
func InspectBankSnapshotV1(raw []byte) (BankSnapshotV1Inspection, error) {
	snapshot, err := DecodeBankSnapshotV1(raw)
	if err != nil {
		return BankSnapshotV1Inspection{}, err
	}
	return BankSnapshotV1Inspection{Snapshot: snapshot, Source: append([]byte(nil), raw...), SHA256: sha256.Sum256(raw)}, nil
}

// EncodeBankSnapshotV1 emits the canonical CDTO kind-8 envelope.  The result
// is bounded by the same 4 MiB artifact limit as the Rust verifier.
func EncodeBankSnapshotV1(snapshot BankSnapshotV1) ([]byte, error) {
	return encodeBankSnapshotV1(snapshot)
}

func encodeBankSnapshotV1(snapshot BankSnapshotV1) ([]byte, error) {
	if !bankSnapshotOneRoot(snapshot.Root) {
		return nil, ErrBankSnapshotGraphInvalid
	}
	graph, err := encodePlayerSnapshotObjectGraph(snapshot.Root)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrBankSnapshotGraphInvalid, err)
	}
	raw, err := encodePlayerSnapshotRecord(bankSnapshotKind, []playerSnapshotField{{id: 1, tag: playerSnapshotTypeBytes, value: graph}})
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrBankSnapshotMalformed, err)
	}
	if len(raw) > bankSnapshotArtifactLimit {
		return nil, ErrBankSnapshotSizeLimit
	}
	return raw, nil
}

// VerifyBankSnapshotV1 checks an independently supplied whole-artifact digest
// before returning a canonical detached graph.  Digest verification alone is
// not provenance; callers still bind the result to an explicit import receipt.
func VerifyBankSnapshotV1(raw []byte, expectedDigest [sha256.Size]byte) (BankSnapshotV1, error) {
	if len(raw) > bankSnapshotArtifactLimit {
		return BankSnapshotV1{}, ErrBankSnapshotSizeLimit
	}
	if sha256.Sum256(raw) != expectedDigest {
		return BankSnapshotV1{}, ErrBankSnapshotDigestMismatch
	}
	snapshot, err := DecodeBankSnapshotV1(raw)
	if err != nil {
		return BankSnapshotV1{}, err
	}
	canonical, err := EncodeBankSnapshotV1(snapshot)
	if err != nil {
		return BankSnapshotV1{}, err
	}
	if !bytes.Equal(canonical, raw) {
		return BankSnapshotV1{}, ErrBankSnapshotNonCanonical
	}
	return snapshot, nil
}

// ToBankAccount converts the detached root into the canonical Go bank shape.
// The root's i64 Value is the legacy bank balance; only its direct children
// become bank item roots.  The container root itself is never exposed as a
// player item.  Item IDs are supplied explicitly by the caller so this
// conversion cannot claim an account or mint identities from display names.
func (s BankSnapshotV1) ToBankAccount(allocate func() (string, error)) (BankAccount, error) {
	if err := validatePlayerSnapshotObjectGraph(s.Root, true); err != nil {
		return BankAccount{}, fmt.Errorf("%w: %v", ErrBankSnapshotGraphInvalid, err)
	}
	if !bankSnapshotOneRoot(s.Root) {
		return BankAccount{}, ErrBankSnapshotGraphInvalid
	}
	balance := s.Root.Nodes[0].Object.Value
	if balance < 0 || balance > MaxBankBalance {
		return BankAccount{}, fmt.Errorf("%w: bank balance %d", ErrBankSnapshotGraphInvalid, balance)
	}
	children := make([][]int, len(s.Root.Nodes))
	for index, node := range s.Root.Nodes {
		if node.ParentIndex != nil {
			parent := *node.ParentIndex
			if parent >= uint32(len(children)) {
				return BankAccount{}, ErrBankSnapshotGraphInvalid
			}
			children[parent] = append(children[parent], index)
		}
	}
	var build func(int) (LegacyObject, error)
	build = func(index int) (LegacyObject, error) {
		object, err := s.Root.Nodes[index].Object.toLegacyObject()
		if err != nil {
			return LegacyObject{}, fmt.Errorf("%w: item %d: %v", ErrBankSnapshotGraphInvalid, index, err)
		}
		for _, child := range children[index] {
			value, err := build(child)
			if err != nil {
				return LegacyObject{}, err
			}
			object.Contents = append(object.Contents, value)
		}
		return object, nil
	}
	objects := make([]LegacyObject, 0, len(children[0]))
	for _, child := range children[0] {
		object, err := build(child)
		if err != nil {
			return BankAccount{}, err
		}
		objects = append(objects, object)
	}
	var items ItemCollection
	if len(objects) == 0 {
		items = ItemCollection{Items: map[string]Item{}}
	} else {
		var err error
		items, err = ImportItems(objects, allocate)
		if err != nil {
			return BankAccount{}, fmt.Errorf("%w: item conversion: %v", ErrBankSnapshotGraphInvalid, err)
		}
	}
	return BankAccount{Balance: balance, Items: &items}, nil
}

func bankSnapshotOneRoot(graph PlayerSnapshotObjectGraphV1) bool {
	if len(graph.Nodes) == 0 || graph.Nodes[0].ParentIndex != nil {
		return false
	}
	roots := 0
	for _, node := range graph.Nodes {
		if node.ParentIndex == nil {
			roots++
		}
	}
	return roots == 1
}

func clonePlayerSnapshotObjectGraph(graph PlayerSnapshotObjectGraphV1) PlayerSnapshotObjectGraphV1 {
	copyGraph := PlayerSnapshotObjectGraphV1{Nodes: make([]PlayerSnapshotObjectNodeV1, len(graph.Nodes))}
	for index, node := range graph.Nodes {
		copyGraph.Nodes[index] = node
		copyGraph.Nodes[index].Object = node.Object
		if node.ParentIndex != nil {
			parent := *node.ParentIndex
			copyGraph.Nodes[index].ParentIndex = &parent
		}
	}
	return copyGraph
}
