from pathlib import Path
import importlib.util
import sys
sys.dont_write_bytecode = True
import tempfile
import tarfile

module_path = Path(__file__).resolve().parents[1] / 'package.py'
spec = importlib.util.spec_from_file_location('package', module_path)
module = importlib.util.module_from_spec(spec)
spec.loader.exec_module(module)
with tempfile.TemporaryDirectory(prefix='bw-package-') as temporary:
    root = Path(temporary)
    stage = root / 'stage'
    stage.mkdir()
    names = ['Tart-LICENSE', 'boxwarden.entitlements', 'identity.sha256',
             'signed-entitlements.plist', 'signing.txt', 'swift-version.txt',
             'tart-boxwarden-clipboard', 'version.txt']
    for name in names:
        (stage / name).write_bytes(('synthetic ' + name).encode())
    (stage / 'unrelated-private-state').write_text('must never be included')
    first = module.package(stage, root / 'first.tar.gz')
    for name in names:
        (stage / name).chmod(0o600)
    second = module.package(stage, root / 'second.tar.gz')
    assert first == second
    with tarfile.open(root / 'first.tar.gz') as tar:
        assert tar.getnames() == sorted(names)
        for member in tar.getmembers():
            assert member.uid == member.gid == member.mtime == 0
            assert tar.extractfile(member).read() == (stage / member.name).read_bytes()
    try:
        module.package(stage, root / 'first.tar.gz')
        raise AssertionError('existing immutable package was overwritten')
    except FileExistsError:
        pass
print('PASS: deterministic exact staged artifact whitelist and no overwrite')
