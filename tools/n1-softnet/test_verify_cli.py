#!/usr/bin/env python3
"""No-privilege regression checks for verifier pre-execution safety."""
import argparse
import json
import os
from pathlib import Path
import shlex
import stat
import subprocess
import sys
import tempfile
from types import SimpleNamespace

parser = argparse.ArgumentParser()
parser.add_argument('verifier', type=Path)
parser.add_argument('candidate', type=Path, nargs='?')
args = parser.parse_args()
sys.dont_write_bytecode = True
sys.path.insert(0, str(args.verifier.parent))
from build import validate_candidate_metadata  # noqa: E402

# Pure synthetic observation. No file gets a setuid or setgid bit.
synthetic = SimpleNamespace(st_mode=stat.S_IFREG | stat.S_ISUID | 0o755,
                            st_uid=os.getuid(), st_nlink=1)
try:
    validate_candidate_metadata(synthetic, os.getuid())
except RuntimeError as error:
    assert 'setuid' in str(error).lower(), str(error)
else:
    raise AssertionError('synthetic setuid mode was accepted')
print('synthetic privileged-mode metadata rejected without a file')

with tempfile.TemporaryDirectory(prefix='n1-cli-safety-') as scratch:
    base = Path(scratch)
    env = {'HOME': scratch, 'PATH': '/usr/bin:/bin', 'PYTHONDONTWRITEBYTECODE': '1'}
    marker = base / 'wrong-digest.marker'
    fake = base / 'wrong-digest.sh'
    fake.write_text('#!/bin/sh\necho executed > ' + shlex.quote(str(marker)) + '\necho softnet 0.19.0-boxwarden-n1.1\n')
    fake.chmod(0o755)
    result = subprocess.run([sys.executable, str(args.verifier), str(fake)], env=env,
                            text=True, capture_output=True, timeout=10)
    assert result.returncode != 0, result.stdout
    assert 'digest' in result.stderr.lower(), result.stderr
    assert not marker.exists(), 'wrong executable ran before digest verification'
    print('ordinary 0755 wrong executable rejected before marker execution')
    if args.candidate is not None:
        result = subprocess.run([sys.executable, str(args.verifier), str(args.candidate)], env=env,
                                text=True, capture_output=True, timeout=30)
        assert result.returncode == 0, result.stderr
        records = [json.loads(line) for line in result.stdout.splitlines()]
        assert len(records) == 7
        assert records[0]['name'] == 'version' and records[0]['exit'] == 0
        assert all(record['exit'] != 0 for record in records[1:])
        print('verified candidate: version plus six exact rejection cases passed')
