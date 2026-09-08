package world

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

// These values are the legacy bit numbers in src/mtype.h. The C oracle also
// includes that header, so a changed source contract makes this test fail at
// the output comparison rather than silently changing the fixture meaning.
const (
	legacyPNOLDS = 4
	legacyPNOSDS = 5
	legacyPNORNM = 6
	legacyPNOEXT = 7
	legacyPDINVI = 21
	legacyPBLIND = 42
)

type legacyViewOracleCase struct {
	Name    string
	Room    LegacyRoom
	Options ViewOptions
}

func TestRenderRoomEnvironmentAgainstLegacyOracle(t *testing.T) {
	cc, err := exec.LookPath("cc")
	if err != nil {
		t.Skip("legacy differential test requires local cc")
	}

	roomSource, err := os.ReadFile("../../../src/room.c")
	if err != nil {
		t.Fatal(err)
	}
	block, err := legacyEnvironmentBlock(string(roomSource))
	if err != nil {
		t.Fatal(err)
	}

	oracleSource := legacyViewOraclePrelude + block + legacyViewOracleSuffix
	dir := t.TempDir()
	sourcePath := filepath.Join(dir, "view_oracle.c")
	if err := os.WriteFile(sourcePath, []byte(oracleSource), 0600); err != nil {
		t.Fatal(err)
	}
	binaryPath := filepath.Join(dir, "view_oracle")
	srcDir, err := filepath.Abs("../../../src")
	if err != nil {
		t.Fatal(err)
	}
	compileCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	compile := exec.CommandContext(compileCtx, cc, "-std=c99", "-Wall", "-Wextra", "-Werror", "-I", srcDir, sourcePath, "-o", binaryPath)
	if output, err := compile.CombinedOutput(); err != nil {
		if compileCtx.Err() != nil {
			t.Fatalf("compile oracle timeout: %v", compileCtx.Err())
		}
		t.Fatalf("compile oracle: %v: %s", err, output)
	}

	for _, tc := range legacyViewOracleCases() {
		t.Run(tc.Name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			defer cancel()
			output, err := exec.CommandContext(ctx, binaryPath, legacyViewOracleArgs(tc)...).CombinedOutput()
			if err != nil {
				if ctx.Err() != nil {
					t.Fatalf("oracle timeout: %v", ctx.Err())
				}
				t.Fatalf("oracle: %v: %s", err, output)
			}
			got := RenderRoomEnvironment(tc.Room, tc.Options)
			if string(output) != got {
				t.Fatalf("C=%q Go=%q", string(output), got)
			}
		})
	}
}

func legacyEnvironmentBlock(source string) (string, error) {
	function := strings.Index(source, "void display_rom(ply_ptr, rom_ptr)")
	if function < 0 {
		return "", fmt.Errorf("display_rom function missing")
	}
	source = source[function:]
	start := strings.Index(source, "\n\tfd = ply_ptr->fd;")
	if start < 0 {
		return "", fmt.Errorf("display_rom environment start missing")
	}
	start++
	endRelative := strings.Index(source[start:], "\n\tif(!F_ISSET(ply_ptr, PDSCRP)) {")
	if endRelative < 0 {
		return "", fmt.Errorf("display_rom environment boundary missing")
	}
	return source[start : start+endRelative], nil
}

func legacyViewOracleArgs(tc legacyViewOracleCase) []string {
	playerFlags := [8]byte{}
	if tc.Options.Blind {
		setLegacyBit(&playerFlags, legacyPBLIND)
	}
	if tc.Options.HideLong {
		setLegacyBit(&playerFlags, legacyPNOLDS)
	}
	if tc.Options.HideShort {
		setLegacyBit(&playerFlags, legacyPNOSDS)
	}
	if tc.Options.HideName {
		setLegacyBit(&playerFlags, legacyPNORNM)
	}
	if tc.Options.ExitDiagram {
		setLegacyBit(&playerFlags, legacyPNOEXT)
	}
	if tc.Options.DetectInvisible {
		setLegacyBit(&playerFlags, legacyPDINVI)
	}

	args := []string{
		strconv.Itoa(tc.Options.Hour),
		legacyBool(tc.Options.OtherPlayerLight),
		legacyBool(tc.Options.HasLight),
		strconv.Itoa(int(tc.Options.Race)),
		strconv.Itoa(int(tc.Options.Class)),
		legacyFlagsHex(tc.Room.Flags[:]),
		legacyFlagsHex(playerFlags[:]),
		tc.Room.Name,
		legacyDescriptionArg(tc.Room.ShortDescription, tc.Room.ShortDescriptionPresent),
		legacyDescriptionArg(tc.Room.LongDescription, tc.Room.LongDescriptionPresent),
		legacyBool(tc.Options.ExitDiagram),
		strconv.Itoa(len(tc.Room.Exits)),
	}
	for _, exit := range tc.Room.Exits {
		args = append(args, exit.Name, legacyFlagsHex(exit.Flags[:]))
	}
	return args
}

func legacyBool(value bool) string {
	if value {
		return "1"
	}
	return "0"
}

func legacyDescriptionArg(value string, present bool) string {
	if value == "" && !present {
		return "-"
	}
	return value
}

func setLegacyBit(flags *[8]byte, bit int) {
	flags[bit/8] |= 1 << uint(bit%8)
}

func legacyFlagsHex(flags []byte) string {
	var value uint64
	for i, b := range flags {
		value |= uint64(b) << uint(8*i)
	}
	return fmt.Sprintf("%016x", value)
}

func legacyExitFlags(bits ...int) (flags [4]byte) {
	for _, bit := range bits {
		flags[bit/8] |= 1 << uint(bit%8)
	}
	return flags
}

func legacyRoomFlags(bits ...int) (flags [8]byte) {
	for _, bit := range bits {
		setLegacyBit(&flags, bit)
	}
	return flags
}

func legacyViewOracleCases() []legacyViewOracleCase {
	cases := []legacyViewOracleCase{
		{
			Name: "bright-visible-list",
			Room: LegacyRoom{
				LegacyRoomHeader: LegacyRoomHeader{
					Name: "광장   ",
					Exits: []LegacyExit{
						{Name: "동"},
						{Name: "서", Flags: legacyExitFlags(1)},
						{Name: "비밀", Flags: legacyExitFlags(0)},
					},
				},
				ShortDescription: "짧은 설명",
				LongDescription:  "긴 설명",
			},
			Options: ViewOptions{Hour: 12},
		},
		{
			Name: "dark-no-light",
			Room: LegacyRoom{LegacyRoomHeader: LegacyRoomHeader{
				Name:  "어두운 방",
				Flags: legacyRoomFlags(9),
			}},
			Options: ViewOptions{Hour: 5},
		},
		{
			Name:    "blind-even-with-light",
			Room:    LegacyRoom{LegacyRoomHeader: LegacyRoomHeader{Name: "실명 방"}},
			Options: ViewOptions{Hour: 12, Blind: true, HasLight: true},
		},
		{
			Name: "dark-other-player-light",
			Room: LegacyRoom{LegacyRoomHeader: LegacyRoomHeader{
				Name:  "동료의 등불",
				Flags: legacyRoomFlags(8),
			}},
			Options: ViewOptions{Hour: 12, OtherPlayerLight: true},
		},
		{
			Name: "hidden-exits",
			Room: LegacyRoom{LegacyRoomHeader: LegacyRoomHeader{
				Name: "숨은 길",
				Exits: []LegacyExit{
					{Name: "동"},
					{Name: "서", Flags: legacyExitFlags(1)},
					{Name: "비밀", Flags: legacyExitFlags(0)},
					{Name: "금지", Flags: legacyExitFlags(19)},
				},
			}},
			Options: ViewOptions{Hour: 12},
		},
		{
			Name: "hidden-exits-detected",
			Room: LegacyRoom{LegacyRoomHeader: LegacyRoomHeader{
				Name: "감지된 길",
				Exits: []LegacyExit{
					{Name: "동"},
					{Name: "서", Flags: legacyExitFlags(1)},
					{Name: "비밀", Flags: legacyExitFlags(0)},
					{Name: "금지", Flags: legacyExitFlags(19)},
				},
			}},
			Options: ViewOptions{Hour: 12, DetectInvisible: true},
		},
		{
			Name: "diagram",
			Room: LegacyRoom{LegacyRoomHeader: LegacyRoomHeader{
				Name: "도표",
				Exits: []LegacyExit{
					{Name: "동"}, {Name: "서"}, {Name: "남"}, {Name: "북"},
					{Name: "위"}, {Name: "밑"}, {Name: "문"},
				},
			}},
			Options: ViewOptions{Hour: 12, ExitDiagram: true},
		},
		{
			Name: "hidden-descriptions",
			Room: LegacyRoom{LegacyRoomHeader: LegacyRoomHeader{
				Name: "숨긴 방",
			}, ShortDescription: "짧은", LongDescription: "긴"},
			Options: ViewOptions{Hour: 12, HideName: true, HideShort: true, HideLong: true},
		},
	}
	for _, v := range []ViewOptions{
		{Hour: 5}, {Hour: 6}, {Hour: 20}, {Hour: 21},
		{Hour: 0, HasLight: true}, {Hour: 0, Race: 1}, {Hour: 0, Race: 2},
		{Hour: 0, Class: 9}, {Hour: 0, Class: 10},
	} {
		cases = append(cases, legacyViewOracleCase{Name: fmt.Sprintf("night-boundary-%d-%d-%d-%t", v.Hour, v.Race, v.Class, v.HasLight), Room: LegacyRoom{LegacyRoomHeader: LegacyRoomHeader{Name: "야간", Flags: legacyRoomFlags(9)}}, Options: v})
	}
	cases = append(cases, legacyViewOracleCase{Name: "present-empty-description", Room: LegacyRoom{LegacyRoomHeader: LegacyRoomHeader{Name: "빈 설명"}, ShortDescriptionPresent: true, LongDescriptionPresent: true}})
	return cases
}

const legacyViewOraclePrelude = `#include <stdarg.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>

#include "mtype.h"

#undef ANSI
#define ANSI(fd, color) ((void)0)

typedef struct exit_ exit_;
typedef struct xtag xtag;
typedef struct ctag ctag;
typedef struct creature creature;
typedef struct room room;

struct exit_ {
    char name[20];
    unsigned char flags[4];
};
struct xtag {
    xtag *next_tag;
    exit_ *ext;
};
struct ctag {
    ctag *next_tag;
    creature *crt;
};
struct creature {
    int fd;
    unsigned char flags[8];
    int race;
    int class;
    int fixture_light;
};
struct room {
    char name[80];
    char *short_desc;
    char *long_desc;
    unsigned char flags[8];
    xtag *first_ext;
    ctag *first_ply;
};

long Time;

static int has_light(creature *crt_ptr) {
    return crt_ptr && crt_ptr->fixture_light;
}

static char *cut_space(char *value) {
    static char buf[512];
    int i;
    strcpy(buf, value);
    for(i = (int)strlen(buf) - 1; i >= 0; --i) {
        if(buf[i] != ' ') break;
    }
    buf[i + 1] = 0;
    return buf;
}

static void print(int fd, const char *format, ...) {
    va_list ap;
    (void)fd;
    va_start(ap, format);
    vprintf(format, ap);
    va_end(ap);
}

static void put_flags(unsigned char *dst, size_t size, const char *text) {
    unsigned long long value = strtoull(text, 0, 16);
    size_t i;
    for(i = 0; i < size; ++i)
        dst[i] = (unsigned char)(value >> (8 * i));
}

static void display_probe(creature *ply_ptr, room *rom_ptr) {
    xtag *xp;
    ctag *cp;
    char str[2048];
    int fd, n = 0, t, light = 0;
    char exit_grp[3][10] = {"       ", "   O   ", "       "};
    int n2;

`

const legacyViewOracleSuffix = `
}

int main(int argc, char **argv) {
    creature player = {0};
    creature other = {0};
    ctag other_tag = {0};
    room room_value = {0};
    int exit_count, i;
    exit_ exits[32] = {{0}};
    xtag tags[32] = {{0}};

    if(argc < 13) return 2;
    Time = strtol(argv[1], 0, 10);
    other.fixture_light = atoi(argv[2]);
    player.fixture_light = atoi(argv[3]);
    player.race = atoi(argv[4]);
    player.class = atoi(argv[5]);
    put_flags(room_value.flags, sizeof(room_value.flags), argv[6]);
    put_flags(player.flags, sizeof(player.flags), argv[7]);
    if(atoi(argv[11])) player.flags[PNOEXT / 8] |= 1 << (PNOEXT % 8);
    snprintf(room_value.name, sizeof(room_value.name), "%s", argv[8]);
    room_value.short_desc = strcmp(argv[9], "-") == 0 ? 0 : argv[9];
    room_value.long_desc = strcmp(argv[10], "-") == 0 ? 0 : argv[10];

    exit_count = atoi(argv[12]);
    if(exit_count < 0 || exit_count > 32 || argc != 13 + exit_count * 2) return 3;
    for(i = 0; i < exit_count; ++i) {
        snprintf(exits[i].name, sizeof(exits[i].name), "%s", argv[13 + i * 2]);
        put_flags(exits[i].flags, sizeof(exits[i].flags), argv[14 + i * 2]);
        tags[i].ext = &exits[i];
        tags[i].next_tag = i + 1 < exit_count ? &tags[i + 1] : 0;
    }
    room_value.first_ext = exit_count ? &tags[0] : 0;
    if(other.fixture_light) {
        other_tag.crt = &other;
        room_value.first_ply = &other_tag;
    }
    display_probe(&player, &room_value);
    return 0;
}
`
