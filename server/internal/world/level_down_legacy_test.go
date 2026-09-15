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

// Compile the real down_level and its real tables, not a second translation.
// Inputs stay within defined signed field ranges; rejection of malformed state
// in Go is deliberately tested separately from legacy numeric wrapping.
func TestLowerLevelAgainstLegacy(t *testing.T) {
	read := func(path string) []byte {
		t.Helper()
		b, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		return b
	}
	extract := func(b []byte, start, end string) string {
		t.Helper()
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
	global := read("../../../src/global.c")
	classTable := "struct { short hpstart,mpstart,hp,mp,ndice,sdice,pdice; " + extract(global, "} class_stats[13]", "};") + "};"
	cycle := extract(global, "short level_cycle[][10]", "};") + "};"
	function := extract(read("../../../src/player.c"), "void down_level(ply_ptr)", "/********************************")
	header, err := filepath.Abs("../../../src/mtype.h")
	if err != nil {
		t.Fatal(err)
	}
	source := fmt.Sprintf(`#include <stdio.h>
#include %q
typedef struct {
unsigned char level; signed char class;
signed char strength,dexterity,constitution,intelligence,piety;
short hpmax,mpmax,hpcur,mpcur,pdice; char flags[8];
} creature;
%s
%s
%s
int main(void) {
int cl,level,target,buff;
while(scanf("%%d %%d %%d %%d",&cl,&level,&target,&buff)==4) {
creature p={0}; p.class=cl; p.level=level;
p.strength=p.dexterity=p.constitution=p.intelligence=p.piety=100;
p.hpmax=2000; p.mpmax=1800; p.hpcur=123; p.mpcur=45; p.pdice=10;
if(buff) F_SET((&p),PUPDMG);
while(p.level>target) down_level(&p);
printf("%%d %%d %%d %%d %%d %%d %%d %%d %%d %%d %%d %%d\n",
p.level,p.hpmax,p.mpmax,p.hpcur,p.mpcur,p.pdice,
p.strength,p.dexterity,p.constitution,p.intelligence,p.piety,!!F_ISSET((&p),PUPDMG));
} return 0;
}`, header, classTable, cycle, function)
	dir := t.TempDir()
	path, binary := filepath.Join(dir, "down.c"), filepath.Join(dir, "down")
	if err := os.WriteFile(path, []byte(source), 0600); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if out, err := exec.CommandContext(ctx, "cc", "-std=c99", path, "-o", binary).CombinedOutput(); err != nil {
		t.Fatalf("%s: %v", out, err)
	}
	var input strings.Builder
	var expected [][12]int
	for cl := 1; cl <= 12; cl++ {
		for level := 1; level <= 255; level++ {
			for _, target := range []int{level, max(1, level-1), max(1, level-4), 1} {
				for buff := 0; buff < 2; buff++ {
					p := LegacyMonster{Class: byte(cl), Level: byte(level), Stats: [5]byte{100, 100, 100, 100, 100}, HPMax: 2000, MPMax: 1800, HPCurrent: 123, MPCurrent: 45, DicePlus: 10}
					if buff == 1 {
						p.Flags[7] |= 8
					}
					got, err := LowerPlayerLevel(p, target)
					if err != nil {
						t.Fatalf("class%d level%d target%d: %v", cl, level, target, err)
					}
					left := 0
					if flag(got.Flags[:], 59) {
						left = 1
					}
					expected = append(expected, [12]int{int(got.Level), int(got.HPMax), int(got.MPMax), int(got.HPCurrent), int(got.MPCurrent), int(got.DicePlus), int(got.Stats[0]), int(got.Stats[1]), int(got.Stats[2]), int(got.Stats[3]), int(got.Stats[4]), left})
					fmt.Fprintln(&input, cl, level, target, buff)
				}
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
	for i, want := range expected {
		var got [12]int
		for j := range got {
			if _, err := fmt.Fscan(r, &got[j]); err != nil {
				t.Fatal(err)
			}
		}
		if got != want {
			t.Fatalf("case%d C%v Go%v", i, got, want)
		}
	}
	var extra string
	if _, err := fmt.Fscan(r, &extra); err != io.EOF {
		t.Fatalf("unexpected trailing output %q: %v", extra, err)
	}
	t.Logf("compared %d level-down cases against C", len(expected))
}
