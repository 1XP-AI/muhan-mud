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

func TestThacoAgainstLegacy(t *testing.T) {
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
			t.Fatal(start)
		}
		tail := raw[at:]
		stop := bytes.Index(tail, []byte(end))
		if stop < 0 {
			t.Fatal(end)
		}
		return string(tail[:stop])
	}
	functions := ""
	for _, start := range []string{"void compute_thaco(ply_ptr)", "int mod_profic(ply_ptr)", "int profic(ply_ptr, index)"} {
		functions += extract(player, start, "/********************************")
	}
	tables := extract(global, "int bonus[64]", "};") + "};\n" + extract(global, "short thaco_list[][20]", "};") + "};\n"
	header, err := filepath.Abs("../../../src/mtype.h")
	if err != nil {
		t.Fatal(err)
	}
	source := fmt.Sprintf(`#include <stdio.h>
#include <stdint.h>
#include %q
#define MAX(a,b) ((a)>(b)?(a):(b))
typedef struct { signed char adjustment,type; } object;
typedef struct { unsigned char level; signed char class,strength,thaco; char flags[8]; object *ready[20]; int32_t proficiency[5]; } creature;
int profic(creature*,int); int mod_profic(creature*);
%s
#define long int32_t
%s
int main(void){int c,l,s,kind,bless,i;while(scanf("%%d %%d %%d %%d %%d",&c,&l,&s,&kind,&bless)==5){creature p={0};object w={0};p.class=c;p.level=l;p.strength=s;w.type=kind;w.adjustment=2;if(kind>=0)p.ready[19]=&w;if(bless)F_SET((&p),PBLESS);for(i=0;i<5;i++)p.proficiency[i]=1000000+i*1000;compute_thaco(&p);printf("%%d\n",p.thaco);}return 0;}
`, header, tables, functions)
	dir := t.TempDir()
	path := filepath.Join(dir, "thaco.c")
	binary := filepath.Join(dir, "thaco")
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
	for class := 1; class <= 12; class++ {
		for _, level := range []byte{1, 4, 5, 80, 100, 101, 255} {
			for _, strength := range []byte{0, 10, 63} {
				for _, kind := range []int{-1, 0, 4, 5} {
					for bless := 0; bless < 2; bless++ {
						p := LegacyMonster{Class: byte(class), Level: level, Stats: [5]byte{strength}}
						if bless == 1 {
							p.Flags[0] |= 1
						}
						for i := range p.Proficiency {
							p.Proficiency[i] = int32(1000000 + i*1000)
						}
						var w *LegacyObject
						if kind >= 0 {
							w = &LegacyObject{Type: byte(kind), Adjustment: 2}
						}
						got, err := ComputeThaco(p, w)
						if err != nil {
							t.Fatal(err)
						}
						expected = append(expected, got)
						fmt.Fprintln(&input, class, level, strength, kind, bless)
					}
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
		var got int
		if _, err := fmt.Fscan(r, &got); err != nil {
			t.Fatal(err)
		}
		if got != int(want) {
			t.Fatalf("case%d C%d Go%d", i, got, want)
		}
	}
}
