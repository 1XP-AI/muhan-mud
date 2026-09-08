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

// Compares numeric state/RNG consumption through the FIRST die call. die is a
// stop adapter, not a simulation of death/respawn. ANSI/messages are not compared.
func TestVitalsAgainstLegacy(t *testing.T) {
	testVitalsLegacy(t, false)
}

// Death is a deterministic substitution, not the real C die implementation.
// This checks the real healing phase's control flow AFTER that substitution.
func TestVitalsResumeAgainstLegacy(t *testing.T) {
	testVitalsLegacy(t, true)
}

func testVitalsLegacy(t *testing.T, resume bool) {
	p, err := os.ReadFile("../../../src/player.c")
	if err != nil {
		t.Fatal(err)
	}
	g, err := os.ReadFile("../../../src/global.c")
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
	phase := extract(p, "/* check if player is suffering from any  aliment */", "if(t > LT(ply_ptr, LT_PSAVE))")
	table := extract(g, "int bonus[64]", "};") + "};"
	header, err := filepath.Abs("../../../src/mtype.h")
	if err != nil {
		t.Fatal(err)
	}
	source := fmt.Sprintf(`#include <stdio.h>
#include <stdint.h>
#include <string.h>
#include <setjmp.h>
#include %q
#undef mrand
#undef ANSI
#define ANSI(...) ((void)0)
#define print(...) ((void)0)
#define time(x) 1
typedef struct { char flags[8]; } room;
typedef struct { signed char class,constitution,intelligence,piety; short hpcur,hpmax,mpcur,mpmax; char flags[8]; room *parent_rom; struct {int32_t ltime,interval;} lasttime[45]; } creature;
static creature body;static room location;static jmp_buf death_stop;static int dead,mode,calls;
int mrand(int lo,int hi){calls++;return mode==2?(calls%%2?hi:lo):(mode?hi:lo);}
int dice(int n,int sides,int plus){while(n--)plus+=mrand(1,sides);return plus;}
void die(creature*a,creature*b){dead=1;longjmp(death_stop,1);}
%s
void phase(creature *ply_ptr){int32_t t=1;char ill,prot=1;
%s
}
int main(void){int c,con,pty,hp,ailment,i;unsigned int flags;
while(scanf("%%d %%d %%d %%u %%d %%d %%d",&c,&con,&pty,&flags,&ailment,&mode,&hp)==7){
memset(&body,0,sizeof(body));memset(&location,0,sizeof(location));dead=0;calls=0;
body.class=c;body.constitution=con;body.intelligence=18;body.piety=pty;body.hpcur=hp;body.hpmax=100;body.mpcur=5;body.mpmax=100;body.parent_rom=&location;
for(i=0;i<4;i++)location.flags[i]=(flags>>(8*i))&255;
if(ailment&1)F_SET((&body),PPOISN);if(ailment&2)F_SET((&body),PDISEA);
if(!setjmp(death_stop))phase(&body);
printf("%%d %%d %%d %%d %%d %%d %%d %%d %%d\n",body.hpcur,body.mpcur,body.lasttime[8].ltime,body.lasttime[8].interval,body.lasttime[3].ltime,body.lasttime[3].interval,!!F_ISSET((&body),PPOISN),dead,calls);
}return 0;}
`, header, table, phase)
	if resume {
		source = strings.Replace(source, "void die(creature*a,creature*b){dead=1;longjmp(death_stop,1);}", `static room respawn;
void die(creature*a,creature*b){dead++;a->hpmax=90;a->hpcur=90;a->mpmax=70;a->mpcur=10;
a->constitution=10;a->piety=20;a->intelligence=16;F_CLR(a,PPOISN);F_CLR(a,PDISEA);a->parent_rom=&respawn;}`, 1)
	}
	dir := t.TempDir()
	path := filepath.Join(dir, "vitals.c")
	binary := filepath.Join(dir, "vitals")
	if err := os.WriteFile(path, []byte(source), 0600); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if out, err := exec.CommandContext(ctx, "cc", "-std=c99", path, "-o", binary).CombinedOutput(); err != nil {
		t.Fatalf("%s %v", out, err)
	}
	type state [9]int
	var input strings.Builder
	var expected []state
	harm := uint32(1 << 24)
	rooms := []uint32{0, 1 << 13, harm, harm | 1<<25, harm | 1<<26, harm | 1<<27, harm | 1<<21, harm | 1<<25 | 1<<27 | 1<<22}
	for _, class := range []byte{2, 5} {
		for _, con := range []byte{0, 10, 63} {
			for _, pty := range []byte{0, 20} {
				for _, flags := range rooms {
					for ailment := 0; ailment < 4; ailment++ {
						for mode := 0; mode < 3; mode++ {
							for _, hp := range []int16{1, 20} {
								player := vitalPlayer()
								player.Class = class
								player.Stats[2] = con
								player.Stats[3] = 18
								player.Stats[4] = pty
								player.HPCurrent = hp
								if ailment&1 != 0 {
									enableEffect(&player, 16)
								}
								if ailment&2 != 0 {
									enableEffect(&player, 41)
								}
								room := LegacyRoom{}
								for i := 0; i < 4; i++ {
									room.Flags[i] = byte(flags >> (8 * i))
								}
								calls := 0
								deaths := 0
								var continuation func(LegacyMonster) (LegacyMonster, *LegacyRoom, error)
								if resume {
									continuation = func(p LegacyMonster) (LegacyMonster, *LegacyRoom, error) {
										deaths++
										p.HPMax = 90
										p.HPCurrent = 90
										p.MPMax = 70
										p.MPCurrent = 10
										p.Stats[2] = 10
										p.Stats[4] = 20
										p.Stats[3] = 16
										p.Flags[2] &^= 1
										p.Flags[5] &^= 2
										return p, &LegacyRoom{}, nil
									}
								}
								result, err := PlanVitalsWithDeath(player, &room, 1, func(lo, hi int) int {
									calls++
									if mode == 2 {
										if calls%2 == 1 {
											return hi
										}
										return lo
									}
									if mode == 1 {
										return hi
									}
									return lo
								}, continuation)
								if err != nil {
									t.Fatal(err)
								}
								poison, death := 0, 0
								if flag(result.Player.Flags[:], 16) {
									poison = 1
								}
								if result.Death {
									death = 1
								}
								if resume {
									death = deaths
								}
								body := result.Player
								expected = append(expected, state{int(body.HPCurrent), int(body.MPCurrent), int(body.Timers[8].LastTime), int(body.Timers[8].Interval), int(body.Timers[3].LastTime), int(body.Timers[3].Interval), poison, death, calls})
								fmt.Fprintln(&input, class, con, pty, flags, ailment, mode, hp)
							}
						}
					}
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
	r := bytes.NewReader(output)
	for i, want := range expected {
		var got state
		for j := range got {
			if _, err := fmt.Fscan(r, &got[j]); err != nil {
				t.Fatal(err)
			}
		}
		if got != want {
			t.Fatalf("case %d C%v Go%v", i, got, want)
		}
	}
	t.Logf("compared %d cases; resume=%v", len(expected), resume)
}
