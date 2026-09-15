package world

import "testing"

func TestTransferUsesStoredVisibilityAndClass(t *testing.T) {
	for _, kind := range []string{"detect", "invisible", "class"} {
		t.Run(kind, func(t *testing.T) {
			s, in := canonicalTransferFixture()
			p := s.Players[in.ActorID]
			p.Body.Class, p.Body.Level = 4, 1
			src := s.Rooms[1]
			if kind == "detect" {
				src.Resource.Exits[0].Flags[0] |= 2
				in.Movement.DetectInvisible = true
			} else {
				src.Resource.Exits[0].Flags[2] |= 4 // XPGUAR
				src.Resource.Monsters = []LegacyMonster{{Flags: [8]byte{0, 0, 0, 0, 64}}}
				in.Movement.Traversal.Invisible = true
				in.Movement.Traversal.Class, in.Movement.Visitor.Class = 12, 12
			}
			s.Players[in.ActorID], s.Rooms[1] = p, src
			_, result, err := s.TransferWithIDs(in, nil, nil, nil)
			if err != nil || result.Movement.Moved {
				t.Fatalf("stale visibility bypass: %+v %v", result.Movement, err)
			}
			switch kind {
			case "detect":
				p.Body.Flags[2] |= 32
				in.Movement.DetectInvisible = false
			case "invisible":
				p.Body.Flags[0] |= 4
			case "class":
				p.Body.Class = 11
			}
			in.Movement.Traversal.Invisible = false
			in.Movement.Traversal.Class, in.Movement.Visitor.Class = 4, 4
			s.Players[in.ActorID] = p
			_, result, err = s.TransferWithIDs(in, nil, nil, nil)
			if err != nil || !result.Movement.Moved {
				t.Fatalf("saved visibility ignored: %+v %v", result.Movement, err)
			}
		})
	}
}

func TestTransferUsesStoredLevitationAndBlindness(t *testing.T) {
	s, in := canonicalTransferFixture()
	p := s.Players[in.ActorID]
	p.Body.Flags[3] |= 2 // PLEVIT
	src := s.Rooms[1]
	src.Resource.Exits[0].Flags[1] |= 1
	s.Players[in.ActorID], s.Rooms[1] = p, src
	_, result, err := s.DirectionalTransferWithIDs(in, nil, func(int, int) int { t.Fatal("levitating actor rolled fall"); return 1 }, nil)
	if err != nil || !result.Movement.Moved {
		t.Fatalf("levitation ignored: %+v %v", result.Movement, err)
	}
	s, in = canonicalTransferFixture()
	p = s.Players[in.ActorID]
	p.Body.Class, p.Body.Level, p.Body.Stats[1] = 1, 100, 10
	p.Body.Flags[0], p.Body.Flags[5] = 2, 4 // hidden and blind
	p.Items = &ItemCollection{Items: map[string]Item{}}
	s.Players[in.ActorID] = p
	_, result, err = s.DirectionalTransferWithIDs(in, nil, func(int, int) int { return 21 }, nil)
	if err != nil || !result.Movement.Moved || result.Movement.Hidden {
		t.Fatalf("blind stealth cap ignored: %+v %v", result.Movement, err)
	}
}

func TestTransferUsesStoredMovementPermissions(t *testing.T) {
	for _, directional := range []bool{false, true} {
		for _, kind := range []string{"silence", "flight", "sex", "level", "family", "family-id", "marriage-id"} {
			t.Run(kind+map[bool]string{false: "/go", true: "/move"}[directional], func(t *testing.T) {
				s, in := canonicalTransferFixture()
				p := s.Players[in.ActorID]
				p.Body.Class, p.Body.Level, p.Body.Stats[1] = 4, 1, 10
				p.Items = &ItemCollection{Items: map[string]Item{}}
				src, dst := s.Rooms[1], s.Rooms[2]
				switch kind {
				case "silence":
					p.Body.Flags[5] |= 16
				case "flight":
					src.Resource.Exits[0].Flags[1] |= 8
					in.Movement.Traversal.Flying = true
				case "sex":
					src.Resource.Exits[0].Flags[1] |= 16
					p.Body.Flags[1] |= 16
				case "level":
					dst.Resource.LowLevel = 10
					in.Movement.Visitor.Level = 100
				case "family":
					dst.Resource.Flags[4] |= 32
					in.Movement.Visitor.FamilyMember = true
				case "family-id":
					dst.Resource.Flags[4] |= 64
					dst.Resource.Special = 7
					in.Movement.Visitor.FamilyID = 7
				case "marriage-id":
					dst.Resource.Flags[5] |= 1
					dst.Resource.Special = 7
					in.Movement.Visitor.MarriageID = 7
				}
				s.Players[in.ActorID], s.Rooms[1], s.Rooms[2] = p, src, dst
				move := s.TransferWithIDs
				if directional {
					move = s.DirectionalTransferWithIDs
				}
				next, result, err := move(in, nil, nil, nil)
				if err != nil || result.Movement.Moved || next.Players[in.ActorID].Body.RoomID != 1 {
					t.Fatalf("stored permission ignored: moved=%v err=%v", result.Movement.Moved, err)
				}
				// Flip only saved permissions; stale caller projections must not deny
				// a move that the authoritative character is allowed to make.
				in.Movement.Traversal.Flying = false
				in.Movement.Visitor.Level = 0
				in.Movement.Visitor.FamilyMember = false
				in.Movement.Visitor.FamilyID, in.Movement.Visitor.MarriageID = 0, 0
				switch kind {
				case "silence":
					p.Body.Flags[5] &^= 16
					in.Movement.Traversal.Immobile = true
				case "flight":
					p.Body.Flags[3] |= 128
				case "sex":
					p.Body.Flags[1] &^= 16
					in.Movement.Traversal.Male = true
				case "level":
					p.Body.Level = 10
				case "family":
					p.Body.Flags[6] |= 128
				case "family-id":
					p.Body.Daily[9].Max = 7
				case "marriage-id":
					p.Body.Daily[8].Max = 7
				}
				s.Players[in.ActorID] = p
				move = s.TransferWithIDs
				if directional {
					move = s.DirectionalTransferWithIDs
				}
				next, result, err = move(in, nil, nil, nil)
				if err != nil || !result.Movement.Moved || next.Players[in.ActorID].Body.RoomID != 2 {
					t.Fatalf("stored permission not honored: moved=%v err=%v", result.Movement.Moved, err)
				}
			})
		}
	}
}
