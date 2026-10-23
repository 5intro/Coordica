"""A repository module must not shadow the required checker's stdlib imports."""
import os
from pathlib import Path
import subprocess
import sys
import tempfile
import unittest


class QualityIsolationTests(unittest.TestCase):
    def test_required_entrypoints_ignore_shadowable_repository_imports(self):
        root = Path(__file__).resolve().parent.parent
        self.assertIn('$(PYTHON) -I scripts/check-quality.py', (root / 'Makefile').read_text())
        self.assertIn('run: python -I scripts/check-quality.py', (root / '.github/workflows/ci.yml').read_text())
        with tempfile.TemporaryDirectory() as directory:
            parent = Path(directory)
            scripts = parent / 'scripts'
            scripts.mkdir()
            checker = scripts / 'check-quality.py'
            checker.write_text((root / 'scripts/check-quality.py').read_text())
            sentinel = scripts / 'shadow-imported'
            shadow = '''from pathlib import Path
from types import SimpleNamespace
Path(__file__).with_name('shadow-imported').touch()
DEVNULL = -3
def check_output(*args, **kwargs): return 'b' * 40
def run(*args, **kwargs): return SimpleNamespace(returncode=0)
'''
            (scripts / 'subprocess.py').write_text(shadow)
            env = dict(os.environ, CI='true', QUALITY_BASE='a' * 40, PYTHONPATH=str(scripts))
            # Prove the attack fixture would bypass an unisolated entrypoint.
            result = subprocess.run([sys.executable, str(checker)], cwd=parent, env=env,
                                    capture_output=True, timeout=15)
            self.assertEqual(result.returncode, 0, result.stderr.decode(errors='replace'))
            self.assertTrue(sentinel.exists())
            sentinel.unlink()
            result = subprocess.run([sys.executable, '-I', str(checker)], cwd=parent, env=env,
                                    capture_output=True, timeout=15)
            self.assertNotEqual(result.returncode, 0, 'shadowed checker falsely passed without a target commit')
            self.assertFalse(sentinel.exists(), 'repository module was imported by the required entrypoint')
