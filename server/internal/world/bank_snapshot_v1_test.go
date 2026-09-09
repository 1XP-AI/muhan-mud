package world

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"testing"
)

func bankSnapshotFixture() BankSnapshotV1 {
	var root PlayerSnapshotObjectV1
	root.Name[0] = 'b'
	root.Name[1] = 'a'
	root.Name[2] = 'n'
	root.Name[3] = 'k'
	root.Name[4] = 0
	root.Description[0] = 'i'
	root.Description[1] = 0
	root.UseOutput[0] = 0
	root.Keys[0][0] = 'b'
	root.Keys[0][1] = 0
	for i := range root.Keys[1] {
		root.Keys[1][i] = 0
		root.Keys[2][i] = 0
	}
	return BankSnapshotV1{Root: PlayerSnapshotObjectGraphV1{Nodes: []PlayerSnapshotObjectNodeV1{{Object: root, ChildIndex: 0}}}}
}

func TestBankSnapshotV1RoundTripAndDigest(t *testing.T) {
	wire, err := EncodeBankSnapshotV1(bankSnapshotFixture())
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	decoded, err := DecodeBankSnapshotV1(wire)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	canonical, err := EncodeBankSnapshotV1(decoded)
	if err != nil || !bytes.Equal(canonical, wire) {
		t.Fatalf("not byte stable: err=%v", err)
	}
	digest := sha256.Sum256(wire)
	verified, err := VerifyBankSnapshotV1(wire, digest)
	if err != nil || verified.Root.Nodes[0].Object.Name[0] != 'b' {
		t.Fatalf("verify: %+v %v", verified, err)
	}
	inspection, err := InspectBankSnapshotV1(wire)
	if err != nil || !bytes.Equal(inspection.Source, wire) || inspection.SHA256 != digest {
		t.Fatalf("inspection: %+v %v", inspection, err)
	}
	inspection.Source[0] ^= 0xff
	if bytes.Equal(inspection.Source, wire) {
		t.Fatal("inspection source aliases input")
	}
}

func TestBankSnapshotV1RejectsMultipleRootsAndWrongKind(t *testing.T) {
	valid, err := EncodeBankSnapshotV1(bankSnapshotFixture())
	if err != nil {
		t.Fatal(err)
	}
	wrongKind := append([]byte(nil), valid...)
	// The kind is part of the authenticated prefix; update the digest after
	// changing it so the rejection exercises the schema rather than the hash.
	binary.BigEndian.PutUint16(wrongKind[10:12], playerSnapshotKind)
	wrongKindPayloadEnd := playerSnapshotPrefixLength + int(binary.BigEndian.Uint32(wrongKind[12:16]))
	digest := sha256.Sum256(wrongKind[playerSnapshotPrefixLength:wrongKindPayloadEnd])
	copy(wrongKind[wrongKindPayloadEnd:], digest[:])
	if _, err := DecodeBankSnapshotV1(wrongKind); !errors.Is(err, ErrBankSnapshotMalformed) {
		t.Fatalf("wrong kind accepted: %v", err)
	}

	graph := bankSnapshotFixture()
	second := graph.Root.Nodes[0]
	graph.Root.Nodes = append(graph.Root.Nodes, second)
	graph.Root.Nodes[1].ParentIndex = nil
	if _, err := EncodeBankSnapshotV1(graph); !errors.Is(err, ErrBankSnapshotGraphInvalid) {
		t.Fatalf("multiple roots accepted: %v", err)
	}
}

func TestBankSnapshotV1RejectsDigestAndSize(t *testing.T) {
	valid, err := EncodeBankSnapshotV1(bankSnapshotFixture())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := VerifyBankSnapshotV1(valid, [sha256.Size]byte{}); !errors.Is(err, ErrBankSnapshotDigestMismatch) {
		t.Fatalf("digest mismatch not rejected: %v", err)
	}
	oversized := make([]byte, bankSnapshotArtifactLimit+1)
	if _, err := DecodeBankSnapshotV1(oversized); !errors.Is(err, ErrBankSnapshotSizeLimit) {
		t.Fatalf("size limit not rejected: %v", err)
	}
}
