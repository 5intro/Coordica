"""Exercise CI's complementary filters against the actual Go test runner."""
from collections import Counter
import importlib.util
import os
from pathlib import Path
import re
import shutil
import tempfile
import unittest
from unittest.mock import patch

spec = importlib.util.spec_from_file_location("ci_batch_guard", Path(__file__).with_name("test-go.py"))
guard = importlib.util.module_from_spec(spec)
spec.loader.exec_module(guard)

FIXTURE = '''package fixture
import ("os"; "testing")
func record(name string) {
    f, err := os.OpenFile(os.Getenv("BATCH_RECORD"), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0600)
    if err != nil { panic(err) }; defer f.Close()
    if _, err = f.WriteString(name+"\\n"); err != nil { panic(err) }
}
func sample(t *testing.T) {
    record(t.Name())
    if os.Getenv("BATCH_FAILURE") == t.Name() { t.Fatal("intentional fixture failure") }
}
func TestAlpha(t *testing.T) { sample(t); t.Run("TestZulu", sample) }
func TestMiddle(t *testing.T) { sample(t) }
func TestZulu(t *testing.T) { sample(t); t.Run("TestAlpha", sample) }
func Test_Extra(t *testing.T) { sample(t) }
func TestΩ(t *testing.T) { sample(t) }
func FuzzAlpha(f *testing.F) {
    f.Add("seed")
    f.Fuzz(func(t *testing.T, v string) { record(t.Name()) })
}
func Example() { record("Example"); // Output:
}
'''


class CIBatchTests(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory()
        self.addCleanup(self.temp.cleanup)
        self.parent = Path(self.temp.name).resolve()
        self.module = self.parent / "fixture"
        self.module.mkdir()
        (self.module / "go.mod").write_text("module fixture\n\ngo 1.23\n")
        (self.module / "fixture_test.go").write_text(FIXTURE, encoding="utf-8")
        self.record = self.parent / "executed.txt"
        self.go = guard.REPO / ".tools/go/bin/go"
        if not self.go.exists():
            self.go = shutil.which("go")
        self.assertTrue(self.go, "Go is required to verify CI coverage")
        workflow = (guard.REPO / ".github/workflows/ci.yml").read_text()
        self.pattern = re.search(r"GO_TEST_BATCH_PATTERN: '([^']+)'", workflow)[1]
        for flag in ("-skip", "-run"):
            self.assertIn(flag + " '${{ env.GO_TEST_BATCH_PATTERN }}' ./...", workflow)

    def batch(self, flag, failure=""):
        with patch.dict(os.environ, BATCH_RECORD=str(self.record), BATCH_FAILURE=failure):
            code = guard.run([str(self.go), "-C", str(self.module), "test", "-race", "-count=1", "-timeout=30s",
                              flag, self.pattern, "./..."], self.parent, 120, 8 * guard.GIB, 4 * guard.GIB)
        self.assertEqual(list(self.parent.glob(guard.PREFIX + "*")), [])
        return code

    def test_union_runs_each_test_example_and_fuzz_seed_once(self):
        self.assertEqual(self.batch("-skip"), 0)
        self.assertEqual(self.batch("-run"), 0)
        expected = ["TestAlpha", "TestAlpha/TestZulu", "TestMiddle", "TestZulu", "TestZulu/TestAlpha",
                    "Test_Extra", "TestΩ", "FuzzAlpha/seed#0", "Example"]
        self.assertEqual(Counter(self.record.read_text(encoding="utf-8").splitlines()), Counter(expected))

    def test_failure_in_either_half_fails_its_batch(self):
        self.assertEqual(self.batch("-skip", "TestZulu"), 1)
        self.assertEqual(self.batch("-run", "TestAlpha"), 1)


if __name__ == "__main__":
    unittest.main()
