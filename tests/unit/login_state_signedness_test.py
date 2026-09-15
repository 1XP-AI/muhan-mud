#!/usr/bin/env python3
"""Source-bound callback-state regression under both compiler char defaults."""
import os
from pathlib import Path
import re
import shlex
import subprocess
import tempfile

root = Path(__file__).resolve().parents[2]
source = (root / 'src/mstruct.h').read_text()
field = re.search(r'^\s*(?:(?:signed|unsigned)\s+)?char\s+fnparam;', source, re.M)
assert field, 'iobuf callback state declaration missing'
program = '''
#include <assert.h>
struct state { %s };
int main(void) {
    struct state io;
    int values[] = {-1, 0, 1, 7, 127};
    unsigned int i;
    assert(sizeof(io.fnparam) == 1);
    for (i = 0; i < sizeof(values)/sizeof(values[0]); i++) {
        io.fnparam = values[i];
        /* Same integer promotion as the actual callback dispatch. */
        assert((int)io.fnparam == values[i]);
    }
    return 0;
}
''' % field.group()
with tempfile.TemporaryDirectory(prefix='muhan-login-signedness-') as directory:
    fixture = Path(directory) / 'state.c'
    binary = Path(directory) / 'state'
    fixture.write_text(program)
    for mode in ['-fsigned-char', '-funsigned-char']:
        subprocess.run(shlex.split(os.environ.get('CC', 'cc')) +
                       ['-std=c99', mode, str(fixture), '-o', str(binary)], check=True)
        subprocess.run([str(binary)], check=True, cwd=directory)
print('login_state_signedness_test: 5 callback states under both char defaults passed')
