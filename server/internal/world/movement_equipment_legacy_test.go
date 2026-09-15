package world

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"
)

type movementLegacyObject struct {
	weight, pdice int16
	flags         uint64
	firstChild    int
	nextSibling   int
}

type movementLegacyInput struct {
	dexterity int
	objects   []movementLegacyObject
	inventory []int
	ready     [20]int
}

type movementLegacyExpected struct {
	name   string
	weight int
	fall   int32
	bonus  int32
}

func movementFlags(bits ...int) [8]byte {
	var flags [8]byte
	for _, bit := range bits {
		flags[bit/8] |= 1 << uint(bit%8)
	}
	return flags
}

func movementMask(flags [8]byte) uint64 {
	var mask uint64
	for i, bit := range flags {
		mask |= uint64(bit) << uint(i*8)
	}
	return mask
}

// movementLegacyFixture intentionally keeps the legacy object relationships in
// the canonical ID graph. The C input is derived from this graph so the two
// implementations see exactly the same roots, children, flags, weights and
// ready slots.
func movementLegacyFixture(kind int) ItemCollection {
	c := ItemCollection{Items: map[string]Item{}}
	add := func(id string, object LegacyObject, contents ...string) {
		c.Items[id] = Item{Object: object, Contents: append([]string(nil), contents...)}
	}

	switch kind {
	case 0: // All boundaries together: weightless roots/descendants and climbers.
		add("inventory-root", LegacyObject{Weight: 7}, "inventory-child", "inventory-sibling")
		add("inventory-child", LegacyObject{Weight: -11, Flags: movementFlags(7)}, "inventory-grandchild")
		add("inventory-grandchild", LegacyObject{Weight: 300})
		add("inventory-sibling", LegacyObject{Weight: 41})
		add("inventory-free-root", LegacyObject{Weight: 13, Flags: movementFlags(7)}, "inventory-free-child")
		add("inventory-free-child", LegacyObject{Weight: 600})
		add("ready-a", LegacyObject{Weight: -5, DicePlus: -2, Flags: movementFlags(16)}, "ready-a-child")
		add("ready-a-child", LegacyObject{Weight: 19, Flags: movementFlags(7)})
		add("ready-b", LegacyObject{Weight: 23, DicePlus: 4, Flags: movementFlags(16)}, "ready-b-child")
		add("ready-b-child", LegacyObject{Weight: -17, Flags: movementFlags(7)})
		add("ready-c", LegacyObject{Weight: 29, DicePlus: -6, Flags: movementFlags(7, 16)}, "ready-c-child")
		add("ready-c-child", LegacyObject{Weight: 31})
	case 1: // An inventory weightless root skips its complete subtree.
		add("inventory-root", LegacyObject{Weight: 1000, Flags: movementFlags(7)}, "inventory-child")
		add("inventory-child", LegacyObject{Weight: -1000}, "inventory-grandchild")
		add("inventory-grandchild", LegacyObject{Weight: 3000})
		add("inventory-free-root", LegacyObject{Weight: 7}, "inventory-free-child")
		add("inventory-free-child", LegacyObject{Weight: 11})
		add("ready-a", LegacyObject{Weight: 2, DicePlus: 1, Flags: movementFlags(16)}, "ready-a-child")
		add("ready-a-child", LegacyObject{Weight: 3})
		add("ready-b", LegacyObject{Weight: 4, DicePlus: 2, Flags: movementFlags(16)}, "ready-b-child")
		add("ready-b-child", LegacyObject{Weight: 5})
		add("ready-c", LegacyObject{Weight: 6, DicePlus: 3, Flags: movementFlags(16)})
	case 2: // A ready weightless root is counted, while its child subtree is not.
		add("inventory-root", LegacyObject{Weight: 7}, "inventory-child")
		add("inventory-child", LegacyObject{Weight: 11})
		add("inventory-free-root", LegacyObject{Weight: 5})
		add("ready-a", LegacyObject{Weight: 100, DicePlus: 7, Flags: movementFlags(7, 16)}, "ready-a-child")
		add("ready-a-child", LegacyObject{Weight: 200}, "ready-a-grandchild")
		add("ready-a-grandchild", LegacyObject{Weight: 300})
		add("ready-b", LegacyObject{Weight: 3, DicePlus: -4, Flags: movementFlags(16)}, "ready-b-child")
		add("ready-b-child", LegacyObject{Weight: 50, Flags: movementFlags(7)})
		add("ready-c", LegacyObject{Weight: 4, DicePlus: 2, Flags: movementFlags(16)}, "ready-c-child")
		add("ready-c-child", LegacyObject{Weight: 6})
	case 3: // Weightless descendants are skipped at every object depth.
		add("inventory-root", LegacyObject{Weight: 10}, "inventory-child")
		add("inventory-child", LegacyObject{Weight: 20, Flags: movementFlags(7)}, "inventory-grandchild")
		add("inventory-grandchild", LegacyObject{Weight: 300})
		add("inventory-free-root", LegacyObject{Weight: 1}, "inventory-free-child")
		add("inventory-free-child", LegacyObject{Weight: 2, Flags: movementFlags(7)}, "inventory-free-grandchild")
		add("inventory-free-grandchild", LegacyObject{Weight: 400})
		add("ready-a", LegacyObject{Weight: 2, DicePlus: 1, Flags: movementFlags(16)}, "ready-a-child")
		add("ready-a-child", LegacyObject{Weight: 20, Flags: movementFlags(7)}, "ready-a-grandchild")
		add("ready-a-grandchild", LegacyObject{Weight: 500})
		add("ready-b", LegacyObject{Weight: 3, DicePlus: 2, Flags: movementFlags(16)}, "ready-b-child")
		add("ready-b-child", LegacyObject{Weight: 4}, "ready-b-grandchild")
		add("ready-b-grandchild", LegacyObject{Weight: 100, Flags: movementFlags(7)})
		add("ready-c", LegacyObject{Weight: -4, DicePlus: -1, Flags: movementFlags(16)})
	case 4: // Signed short weights and pdice, with several ready climbers.
		add("inventory-root", LegacyObject{Weight: -32768}, "inventory-child")
		add("inventory-child", LegacyObject{Weight: 32767})
		add("inventory-free-root", LegacyObject{Weight: -7})
		add("ready-a", LegacyObject{Weight: -100, DicePlus: -32768, Flags: movementFlags(16)}, "ready-a-child")
		add("ready-a-child", LegacyObject{Weight: 20000})
		add("ready-b", LegacyObject{Weight: 300, DicePlus: 32767, Flags: movementFlags(16)}, "ready-b-child")
		add("ready-b-child", LegacyObject{Weight: -400})
		add("ready-c", LegacyObject{Weight: -5, DicePlus: -1, Flags: movementFlags(16)})
	default:
		panic(fmt.Sprintf("unknown movement fixture %d", kind))
	}

	c.Inventory = []string{"inventory-root", "inventory-free-root"}
	c.Ready[0] = "ready-a"
	c.Ready[1] = "ready-b"
	c.Ready[2] = "ready-c"
	return c
}

func movementLegacyInputFor(c ItemCollection, dexterity byte) (movementLegacyInput, error) {
	if err := c.Validate(); err != nil {
		return movementLegacyInput{}, err
	}
	ids := make([]string, 0, len(c.Items))
	for id := range c.Items {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	index := make(map[string]int, len(ids))
	for i, id := range ids {
		index[id] = i
	}
	objects := make([]movementLegacyObject, len(ids))
	for i := range objects {
		objects[i].firstChild = -1
		objects[i].nextSibling = -1
	}
	for i, id := range ids {
		item := c.Items[id]
		objects[i] = movementLegacyObject{
			weight:      item.Object.Weight,
			pdice:       item.Object.DicePlus,
			flags:       movementMask(item.Object.Flags),
			firstChild:  -1,
			nextSibling: -1,
		}
	}
	for i, id := range ids {
		item := c.Items[id]
		if len(item.Contents) > 0 {
			objects[i].firstChild = index[item.Contents[0]]
			for child := range item.Contents {
				childIndex := index[item.Contents[child]]
				if child+1 < len(item.Contents) {
					objects[childIndex].nextSibling = index[item.Contents[child+1]]
				}
			}
		}
	}
	input := movementLegacyInput{dexterity: int(dexterity), objects: objects, ready: [20]int{}}
	for i := range input.ready {
		input.ready[i] = -1
		if c.Ready[i] != "" {
			input.ready[i] = index[c.Ready[i]]
		}
	}
	for _, id := range c.Inventory {
		input.inventory = append(input.inventory, index[id])
	}
	return input, nil
}

func writeMovementLegacyInput(w *strings.Builder, input movementLegacyInput) {
	fmt.Fprintf(w, "%d %d %d", input.dexterity, len(input.objects), len(input.inventory))
	for _, object := range input.objects {
		fmt.Fprintf(w, " %d %d %d %d %d", object.weight, object.pdice, object.flags, object.firstChild, object.nextSibling)
	}
	for _, id := range input.inventory {
		fmt.Fprintf(w, " %d", id)
	}
	for _, id := range input.ready {
		fmt.Fprintf(w, " %d", id)
	}
	w.WriteByte('\n')
}

func TestMovementEquipmentAgainstLegacy(t *testing.T) {
	read := func(path string) []byte {
		t.Helper()
		content, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		return content
	}
	extract := func(content []byte, start, end string) string {
		t.Helper()
		at := bytes.Index(content, []byte(start))
		if at < 0 {
			t.Fatalf("missing legacy source %q", start)
		}
		content = content[at:]
		stop := bytes.Index(content, []byte(end))
		if stop < 0 {
			t.Fatalf("missing legacy source end %q", end)
		}
		return string(content[:stop])
	}
	player := read("../../../src/player.c")
	object := read("../../../src/object.c")
	global := read("../../../src/global.c")
	weightObject := extract(object, "int weight_obj(obj_ptr)", "/************************************************************************")
	weightPlayer := extract(player, "int weight_ply(ply_ptr)", "/********************************")
	fallPlayer := extract(player, "int fall_ply(ply_ptr)", "/********************************")
	bonus := extract(global, "int bonus[64]", "};") + "};"
	header, err := filepath.Abs("../../../src/mtype.h")
	if err != nil {
		t.Fatal(err)
	}
	csource := fmt.Sprintf(`#include <stdio.h>
#include %q

typedef struct object object;
typedef struct obj_tag otag;
typedef struct creature creature;
struct object {
    short weight, pdice;
    char flags[8];
    otag *first_obj;
};
struct obj_tag {
    otag *next_tag;
    object *obj;
};
struct creature {
    char dexterity;
    object *ready[MAXWEAR];
    otag *first_obj;
};

%s
%s
%s
%s

int main(void) {
    int dexterity, object_count, inventory_count;
    while (scanf("%%d %%d %%d", &dexterity, &object_count, &inventory_count) == 3) {
        object objects[64] = {0};
        otag child_tags[64] = {0};
        otag inventory_tags[MAXWEAR] = {0};
        creature player = {0};
        int first_child[64], next_sibling[64], inventory[ MAXWEAR ];
        int ready[MAXWEAR];
        int i, j;
        if (dexterity < 0 || dexterity >= 64 || object_count < 0 || object_count > 64 ||
            inventory_count < 0 || inventory_count > MAXWEAR) return 2;
		for (i = 0; i < object_count; i++) {
		    int weight, pdice;
		    unsigned long long flags;
		    if (scanf("%%d %%d %%llu %%d %%d", &weight, &pdice, &flags,
                      &first_child[i], &next_sibling[i]) != 5) return 3;
            if (weight < -32768 || weight > 32767 || pdice < -32768 || pdice > 32767 ||
                first_child[i] < -1 || first_child[i] >= object_count ||
                next_sibling[i] < -1 || next_sibling[i] >= object_count) return 4;
            objects[i].weight = (short)weight;
            objects[i].pdice = (short)pdice;
            for (j = 0; j < 8; j++) objects[i].flags[j] = (char)((flags >> (j * 8)) & 255);
        }
        for (i = 0; i < inventory_count; i++) {
            if (scanf("%%d", &inventory[i]) != 1 || inventory[i] < 0 || inventory[i] >= object_count) return 5;
        }
        for (i = 0; i < MAXWEAR; i++) {
            if (scanf("%%d", &ready[i]) != 1 || ready[i] < -1 || ready[i] >= object_count) return 6;
        }
        for (i = 0; i < object_count; i++) {
            child_tags[i].obj = &objects[i];
            child_tags[i].next_tag = next_sibling[i] < 0 ? 0 : &child_tags[next_sibling[i]];
            objects[i].first_obj = first_child[i] < 0 ? 0 : &child_tags[first_child[i]];
        }
        for (i = 0; i < inventory_count; i++) {
            inventory_tags[i].obj = &objects[inventory[i]];
            inventory_tags[i].next_tag = i + 1 < inventory_count ? &inventory_tags[i + 1] : 0;
        }
        player.first_obj = inventory_count == 0 ? 0 : &inventory_tags[0];
        player.dexterity = (char)dexterity;
        for (i = 0; i < MAXWEAR; i++) player.ready[i] = ready[i] < 0 ? 0 : &objects[ready[i]];
        printf("%%d %%d %%d\n", weight_ply(&player), fall_ply(&player), bonus[dexterity]);
    }
    return 0;
}
`, header, bonus, weightObject, weightPlayer, fallPlayer)
	dir := t.TempDir()
	sourcePath, binaryPath := filepath.Join(dir, "movement.c"), filepath.Join(dir, "movement")
	if err := os.WriteFile(sourcePath, []byte(csource), 0600); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if output, err := exec.CommandContext(ctx, "cc", "-std=c99", sourcePath, "-o", binaryPath).CombinedOutput(); err != nil {
		t.Fatalf("compile legacy movement oracle: %s: %v", output, err)
	}

	var input strings.Builder
	var expected []movementLegacyExpected
	for fixture := 0; fixture < 5; fixture++ {
		for dexterity := byte(0); dexterity < 64; dexterity++ {
			collection := movementLegacyFixture(fixture)
			stats, err := collection.MovementStats(dexterity)
			if err != nil {
				t.Fatalf("fixture%d dex%d: %v", fixture, dexterity, err)
			}
			legacyInput, err := movementLegacyInputFor(collection, dexterity)
			if err != nil {
				t.Fatalf("fixture%d dex%d encode: %v", fixture, dexterity, err)
			}
			writeMovementLegacyInput(&input, legacyInput)
			expected = append(expected, movementLegacyExpected{
				name:   fmt.Sprintf("fixture%d/dex%d", fixture, dexterity),
				weight: stats.CarriedWeight,
				fall:   stats.FallSkill,
				bonus:  stats.DexterityBonus,
			})
		}
	}
	cmd := exec.CommandContext(ctx, binaryPath)
	cmd.Stdin = strings.NewReader(input.String())
	output, err := cmd.Output()
	if err != nil {
		t.Fatal(err)
	}
	reader := bytes.NewReader(output)
	for _, want := range expected {
		var gotWeight, gotFall, gotBonus int
		if _, err := fmt.Fscan(reader, &gotWeight, &gotFall, &gotBonus); err != nil {
			t.Fatalf("%s: read C output: %v", want.name, err)
		}
		if gotWeight != want.weight || gotFall != int(want.fall) || gotBonus != int(want.bonus) {
			t.Fatalf("%s: C(weight=%d fall=%d bonus=%d) Go(weight=%d fall=%d bonus=%d)", want.name, gotWeight, gotFall, gotBonus, want.weight, want.fall, want.bonus)
		}
	}
	var extra string
	if _, err := fmt.Fscan(reader, &extra); err != io.EOF {
		t.Fatalf("unexpected trailing C output %q: %v", extra, err)
	}
	t.Logf("compared %d movement equipment cases against original C", len(expected))
}
