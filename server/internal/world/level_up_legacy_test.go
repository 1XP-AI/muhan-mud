package world

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// Execute the original function and tables. Single-step cases exercise every
// early-return boundary; cumulative cases exercise repeated stat increases.
// Overflow rejection is a Go policy, tested separately from C signed wrapping.
func TestRaiseLevelAgainstLegacy(t *testing.T) {
	extract := func(path, start, end string) string {
		t.Helper()
		b, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		i := bytes.Index(b, []byte(start))
		if i < 0 {
			t.Fatalf("missing %q", start)
		}
		b = b[i:]
		j := bytes.Index(b, []byte(end))
		if j < 0 {
			t.Fatalf("missing %q", end)
		}
		return string(b[:j])
	}
	classTable := "struct { short hpstart,mpstart,hp,mp,ndice,sdice,pdice; " + extract("../../../src/global.c", "} class_stats[13]", "};") + "};"
	cycle := extract("../../../src/global.c", "short level_cycle[][10]", "};") + "};"
	function := extract("../../../src/player.c", "void up_level(ply_ptr)", "/********************************")
	header, err := filepath.Abs("../../../src/mtype.h")
	if err != nil {
		t.Fatal(err)
	}
	source := fmt.Sprintf(`#include <stdio.h>
#include %q
typedef struct {
unsigned char level; signed char class;
signed char strength,dexterity,constitution,intelligence,piety;
short hpmax,mpmax,hpcur,mpcur,ndice,sdice,pdice;
} creature;
%s
%s
%s
int main(void) {
int cl,level,target;
while(scanf("%%d %%d %%d",&cl,&level,&target)==3) {
creature p={0}; p.class=cl; p.level=level;
p.strength=10; p.dexterity=11; p.constitution=12; p.intelligence=13; p.piety=14;
p.hpmax=2000; p.mpmax=1800; p.hpcur=123; p.mpcur=45;
p.ndice=9; p.sdice=10; p.pdice=11;
while(p.level<target) up_level(&p);
printf("%%d %%d %%d %%d %%d %%d %%d %%d %%d %%d %%d %%d %%d\n",
p.level,p.hpmax,p.mpmax,p.hpcur,p.mpcur,p.ndice,p.sdice,p.pdice,
p.strength,p.dexterity,p.constitution,p.intelligence,p.piety);
} return 0;
}`, header, classTable, cycle, function)
	dir := t.TempDir()
	path, binary := filepath.Join(dir, "up.c"), filepath.Join(dir, "up")
	if err := os.WriteFile(path, []byte(source), 0600); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if out, err := exec.CommandContext(ctx, "cc", "-std=c99", path, "-o", binary).CombinedOutput(); err != nil {
		t.Fatalf("%s: %v", out, err)
	}
	type comparison struct {
		class, level, target int
		values               [13]int
	}
	var expected []comparison
	var input strings.Builder
	for cl := 1; cl <= 12; cl++ {
		for level := 0; level < 255; level++ {
			for _, target := range []int{level + 1, 255} {
				p := LegacyMonster{Class: byte(cl), Level: byte(level), Stats: [5]byte{10, 11, 12, 13, 14}, HPMax: 2000, MPMax: 1800, HPCurrent: 123, MPCurrent: 45, DiceCount: 9, DiceSides: 10, DicePlus: 11}
				for int(p.Level) < target {
					p, err = RaisePlayerLevel(p)
					if err != nil {
						t.Fatalf("class%d level%d target%d: %v", cl, level, target, err)
					}
				}
				expected = append(expected, comparison{cl, level, target, [13]int{int(p.Level), int(p.HPMax), int(p.MPMax), int(p.HPCurrent), int(p.MPCurrent), int(p.DiceCount), int(p.DiceSides), int(p.DicePlus), int(p.Stats[0]), int(p.Stats[1]), int(p.Stats[2]), int(p.Stats[3]), int(p.Stats[4])}})
				fmt.Fprintln(&input, cl, level, target)
			}
		}
	}
	cmd := exec.CommandContext(ctx, binary)
	cmd.Stdin = strings.NewReader(input.String())
	out, err := cmd.Output()
	if err != nil {
		t.Fatal(err)
	}
	r := bytes.NewReader(out)
	for _, want := range expected {
		var got [13]int
		for j := range got {
			if _, err := fmt.Fscan(r, &got[j]); err != nil {
				t.Fatal(err)
			}
		}
		if got != want.values {
			t.Fatalf("class%d level%d target%d C%v Go%v", want.class, want.level, want.target, got, want.values)
		}
	}
	var extra string
	if _, err := fmt.Fscan(r, &extra); err != io.EOF {
		t.Fatalf("trailing output %q: %v", extra, err)
	}
	t.Logf("compared %d level-up cases against original C", len(expected))
}
