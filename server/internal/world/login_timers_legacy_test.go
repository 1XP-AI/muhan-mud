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

func TestLoginTimersAgainstLegacy(t *testing.T) {
	source, err := os.ReadFile("../../../src/player.c")
	if err != nil {
		t.Fatal(err)
	}
	extract := func(start, end string) string {
		at := bytes.Index(source, []byte(start))
		if at < 0 {
			t.Fatalf("missing start %s", start)
		}
		tail := source[at:]
		stop := bytes.Index(tail, []byte(end))
		if stop < 0 {
			t.Fatalf("missing end %s", end)
		}
		return string(tail[:stop])
	}
	save := extract("ply_ptr->lasttime[LT_PSAVE].ltime = t;", "login_time[ply_ptr->fd] = t;")
	shift := extract("tdiff = t -  ply_ptr->lasttime[LT_HOURS].ltime;", "broad_time[ply_ptr->fd]=0;")
	header, err := filepath.Abs("../../../src/mtype.h")
	if err != nil {
		t.Fatal(err)
	}
	csource := fmt.Sprintf(`#include <stdio.h>
#include <stdint.h>
#include %q
#define MIN(a,b) ((a)<(b)?(a):(b))
int main(void) {
struct { struct { int32_t ltime, interval; int16_t misc; } lasttime[45]; } body, *ply_ptr=&body;
int32_t t,tdiff; int i;
if(scanf("%%d",&t)!=1) return 2;
for(i=0;i<45;i++) { int misc; if(scanf("%%d %%d %%d",&body.lasttime[i].ltime,&body.lasttime[i].interval,&misc)!=3) return 3; body.lasttime[i].misc=misc; }
%s
%s
for(i=0;i<45;i++) printf("%%d %%d %%d\n",body.lasttime[i].ltime,body.lasttime[i].interval,body.lasttime[i].misc);
return 0;
}`, header, save, shift)
	dir := t.TempDir()
	path := filepath.Join(dir, "oracle.c")
	binary := filepath.Join(dir, "oracle")
	if err := os.WriteFile(path, []byte(csource), 0600); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if output, err := exec.CommandContext(ctx, "cc", "-std=c99", path, "-o", binary).CombinedOutput(); err != nil {
		t.Fatalf("C compile: %v %s", err, output)
	}
	for _, clock := range [][2]int32{{1000, 900}, {1000, 1100}, {1000, 1000}, {1000, 0}} {
		var before [45]LegacyTimer
		for i := range before {
			before[i] = LegacyTimer{LastTime: int32(700 + i*10), Interval: int32(40 + i), Misc: int16(i)}
		}
		before[28].LastTime = clock[1]
		var input strings.Builder
		fmt.Fprintln(&input, clock[0])
		for _, timer := range before {
			fmt.Fprintln(&input, timer.LastTime, timer.Interval, timer.Misc)
		}
		cmd := exec.CommandContext(ctx, binary)
		cmd.Stdin = strings.NewReader(input.String())
		output, err := cmd.Output()
		if err != nil {
			t.Fatal(err)
		}
		var want [45]LegacyTimer
		reader := bytes.NewReader(output)
		for i := range want {
			if _, err := fmt.Fscan(reader, &want[i].LastTime, &want[i].Interval, &want[i].Misc); err != nil {
				t.Fatal(err)
			}
		}
		got, err := LoginTimers(before, clock[0])
		if err != nil || got != want {
			t.Fatalf("clock %v mismatch: %v", clock, err)
		}
	}
}
