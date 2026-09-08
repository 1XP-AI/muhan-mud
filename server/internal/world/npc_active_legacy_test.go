package world

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestNPCActiveOrderMatchesActualC(t *testing.T) {
	raw, err := os.ReadFile("../../../src/update.c")
	if err != nil {
		t.Fatal(err)
	}
	source := string(raw)
	start := strings.Index(source, "void add_active(crt_ptr)")
	end := strings.Index(source, "void update_exit(t)")
	if start < 0 || end <= start {
		t.Fatal("C active functions not found")
	}
	harness := `#include <stdlib.h>
#include <stdio.h>
typedef struct { int id; } creature;
typedef struct ctag { creature *crt; struct ctag *next_tag; } ctag;
ctag *first_active;
#define FATAL 1
void merror(char *s, int n) { exit(2); }
void del_active(creature *p);
` + source[start:end] + `
int main(void) {
 creature n[4]={{0},{1},{2},{3}}; int op,id;
 while(scanf("%d %d", &op, &id)==2) {
  if(op) add_active(&n[id]); else del_active(&n[id]);
  for(ctag *c=first_active;c;c=c->next_tag) printf("%d,",c->crt->id);
  puts("");
 }
 return 0;
}
`
	dir := t.TempDir()
	path, binary := filepath.Join(dir, "active.c"), filepath.Join(dir, "active")
	if err := os.WriteFile(path, []byte(harness), 0600); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	if out, err := exec.CommandContext(ctx, "cc", "-std=c99", path, "-o", binary).CombinedOutput(); err != nil {
		t.Fatalf("compile: %v %s", err, out)
	}
	var input, expected strings.Builder
	s := State{ActiveNPCIDs: []string{}, Rooms: map[int16]RoomState{}}
	// Exercise reactivation, absent removal and multiple lists deterministically.
	for i := 0; i < 128; i++ {
		id, op := (i*7+i/5)%4, (i/3)%2
		key := fmt.Sprint(id)
		fmt.Fprintf(&input, "%d %d\n", op, id)
		if op == 1 {
			s.activateEntryNPCs(1, false, &npcEntryDelta{SpawnOrder: []string{key}})
		} else {
			s.Rooms[1] = RoomState{NPCIDs: []string{key}}
			s.deactivateRoomNPCs(1)
		}
		for _, active := range s.ActiveNPCIDs {
			fmt.Fprintf(&expected, "%s,", active)
		}
		expected.WriteByte('\n')
	}
	cmd := exec.CommandContext(ctx, binary)
	cmd.Stdin = strings.NewReader(input.String())
	out, err := cmd.CombinedOutput()
	if err != nil || string(out) != expected.String() {
		t.Fatalf("C/Go active order differs: %v\nC %s\nGo %s", err, out, expected.String())
	}
}
