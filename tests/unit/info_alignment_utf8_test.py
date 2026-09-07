#!/usr/bin/env python3
"""Execute the production info alignment block with ASan/UBSan.

This source-bound slice checks UTF-8 bytes and both threshold boundaries;
it does not stand in for the full info command or a live game session.
"""
import os
from pathlib import Path
import re
import shlex
import subprocess
import tempfile

root = Path(__file__).resolve().parents[2]
source = (root / "src/command4.c").read_text(encoding="utf-8")
info = source.split("int info(ply_ptr, cmnd)", 1)[1].split("void info_2(", 1)[0]
declaration = re.search(r"(?:const\s+)?char\s+(?:\*\s*)?alstr(?:\[\d+\])?\s*;", info)
assert declaration is not None, "alignment declaration not found"
start = info.index("        if(ply_ptr->alignment < -100)")
end = info.index("        for(lv=0,cnt=0;", start)
block = info[start:end]
program = """
#include <assert.h>
#include <string.h>
static void check(int alignment, const char *expected) {
    struct { int alignment; } player = {alignment}, *ply_ptr = &player;
%s
%s
    assert(strcmp(alstr, expected) == 0);
}
int main(void) {
    check(-32768, " (악합니다)");
    check(-101, " (악합니다)");
    check(-100, " (평범합니다)");
    check(0, " (평범합니다)");
    check(100, " (평범합니다)");
    check(101, " (선합니다) ");
    check(32767, " (선합니다) ");
    return 0;
}
""" % (declaration.group(), block)
with tempfile.TemporaryDirectory(prefix="muhan-info-alignment-") as directory:
    fixture = Path(directory) / "alignment.c"
    binary = Path(directory) / "alignment"
    fixture.write_text(program, encoding="utf-8")
    # Keep the actual copy observable in the failing version: at -O1 clang
    # can fold strcpy/strcmp away, hiding the overflow from the sanitizer.
    subprocess.run(shlex.split(os.environ.get("CC", "cc")) + [
        "-std=c99", "-O0", "-fno-builtin", "-g", "-fno-omit-frame-pointer",
        "-fsanitize=address,undefined", str(fixture), "-o", str(binary),
    ], check=True)
    subprocess.run([str(binary)], check=True, env={**os.environ,
        "ASAN_OPTIONS": "detect_leaks=0:halt_on_error=1",
        "UBSAN_OPTIONS": "halt_on_error=1"})
print("info_alignment_utf8_test: 7 boundary cases passed with ASan/UBSan")
