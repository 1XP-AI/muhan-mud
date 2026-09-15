package game

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
)

// Compile the actual legacy race-adjustment switch as a test-only oracle.
// Neither this compiler nor the resulting C binary is a runtime dependency.
func TestCreationAgainstLegacyRaceSwitch(t *testing.T) {
	cc, err := exec.LookPath("cc")
	if err != nil {
		t.Skip("legacy differential test requires local cc")
	}
	source, err := os.ReadFile("../../../src/command1.c")
	if err != nil {
		t.Fatal(err)
	}
	header, err := os.ReadFile("../../../src/mtype.h")
	if err != nil {
		t.Fatal(err)
	}
	text := string(source)
	start := strings.Index(text, "switch(Ply[fd].ply->race) {")
	if start < 0 {
		t.Fatal("legacy race switch missing")
	}
	end := strings.Index(text[start:], "\n\n\t\t\t\tprint(fd,")
	if end < 0 {
		t.Fatal("legacy race switch boundary changed")
	}
	block := text[start : start+end]
	var defines strings.Builder
	for _, name := range []string{"DWARF", "ELF", "GNOME", "HALFELF", "HALFGIANT", "HOBBIT", "HUMAN", "ORC"} {
		match := regexp.MustCompile(`(?m)^#define\s+` + name + `\s+(\d+)\s*$`).FindStringSubmatch(string(header))
		if match == nil {
			t.Fatalf("missing legacy constant %s", name)
		}
		fmt.Fprintf(&defines, "#define %s %s\n", name, match[1])
	}
	harness := "#include <stdio.h>\n#include <stdlib.h>\n" + defines.String() + `
struct character { int race,strength,dexterity,constitution,intelligence,piety; };
struct slot { struct character *ply; } Ply[1];
int main(int argc, char **argv) {
 if(argc != 7) return 2;
 struct character value={atoi(argv[1]),atoi(argv[2]),atoi(argv[3]),atoi(argv[4]),atoi(argv[5]),atoi(argv[6])};
 int fd=0; Ply[fd].ply=&value;
` + block + `
 printf("%d %d %d %d %d", value.strength,value.dexterity,value.constitution,value.intelligence,value.piety);
 return 0;
}
`
	dir := t.TempDir()
	path := filepath.Join(dir, "oracle.c")
	if err := os.WriteFile(path, []byte(harness), 0600); err != nil {
		t.Fatal(err)
	}
	binary := filepath.Join(dir, "oracle")
	if output, err := exec.Command(cc, "-std=c99", "-Wall", "-Wextra", "-Werror", path, "-o", binary).CombinedOutput(); err != nil {
		t.Fatalf("compile oracle: %v: %s", err, output)
	}
	for _, base := range []Stats{{12, 10, 12, 10, 10}, {3, 3, 3, 3, 3}, {18, 18, 12, 3, 3}} {
		for race := 1; race <= 8; race++ {
			got, err := BuildCreation(CreationChoices{Class: 4, Stats: base, Weapon: 1, RaceChoice: race})
			if err != nil {
				t.Fatal(err)
			}
			args := []string{strconv.Itoa(got.RaceID)}
			for _, value := range base {
				args = append(args, strconv.Itoa(value))
			}
			output, err := exec.Command(binary, args...).Output()
			if err != nil {
				t.Fatal(err)
			}
			want := fmt.Sprintf("%d %d %d %d %d", got.Stats[0], got.Stats[1], got.Stats[2], got.Stats[3], got.Stats[4])
			if string(output) != want {
				t.Fatalf("race %d C=%s Go=%s", race, output, want)
			}
		}
	}
}
