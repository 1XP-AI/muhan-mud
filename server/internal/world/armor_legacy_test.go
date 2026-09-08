package world

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestArmorAgainstLegacy(t *testing.T) {
	player, err := os.ReadFile("../../../src/player.c")
	if err != nil {
		t.Fatal(err)
	}
	global, err := os.ReadFile("../../../src/global.c")
	if err != nil {
		t.Fatal(err)
	}
	extract := func(raw []byte, start, end string) string {
		at := bytes.Index(raw, []byte(start))
		if at < 0 {
			t.Fatalf("missing %s", start)
		}
		tail := raw[at:]
		stop := bytes.Index(tail, []byte(end))
		if stop < 0 {
			t.Fatalf("missing %s", end)
		}
		return string(tail[:stop])
	}
	function := extract(player, "void compute_ac(ply_ptr)", "/********************************")
	table := extract(global, "int bonus[64]", "};") + "};"
	header, err := filepath.Abs("../../../src/mtype.h")
	if err != nil {
		t.Fatal(err)
	}
	source := fmt.Sprintf(`#include <stdio.h>
#include %q
#define MIN(a,b) ((a)<(b)?(a):(b))
#define MAX(a,b) ((a)>(b)?(a):(b))
typedef struct { signed char armor; } object;
typedef struct { signed char dexterity,armor; char flags[8]; object *ready[20]; } creature;
%s
%s
int main(void) {
int dex,adjust,count,protect,i;
while(scanf("%%d %%d %%d %%d",&dex,&adjust,&count,&protect)==4) {
creature p={0}; object item={0}; item.armor=adjust; p.dexterity=dex;
if(protect) F_SET((&p),PPROTE);
for(i=0;i<count;i++) p.ready[i]=&item;
compute_ac(&p); printf("%%d\n",p.armor);
} return 0;
}`, header, table, function)
	dir := t.TempDir()
	path := filepath.Join(dir, "armor.c")
	binary := filepath.Join(dir, "armor")
	if err := os.WriteFile(path, []byte(source), 0600); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if out, err := exec.CommandContext(ctx, "cc", "-std=c99", path, "-o", binary).CombinedOutput(); err != nil {
		t.Fatalf("%s %v", out, err)
	}
	var input strings.Builder
	var expected []int8
	for dex := 0; dex < 128; dex++ {
		for _, adjust := range []int8{-128, -1, 0, 20, 127} {
			for _, count := range []int{0, 1, 20} {
				for protection := 0; protection < 2; protection++ {
					var ready [20]*LegacyObject
					for i := 0; i < count; i++ {
						ready[i] = &LegacyObject{Armor: byte(adjust)}
					}
					value, err := ComputeArmorClass(byte(dex), ready, protection == 1)
					if err != nil {
						t.Fatal(err)
					}
					expected = append(expected, value)
					fmt.Fprintln(&input, dex, adjust, count, protection)
				}
			}
		}
	}
	cmd := exec.CommandContext(ctx, binary)
	cmd.Stdin = strings.NewReader(input.String())
	output, err := cmd.Output()
	if err != nil {
		t.Fatal(err)
	}
	reader := bytes.NewReader(output)
	for i, want := range expected {
		var got int
		if _, err := fmt.Fscan(reader, &got); err != nil {
			t.Fatal(err)
		}
		if got != int(want) {
			t.Fatalf("case%d C%d Go%d", i, got, want)
		}
	}
}
