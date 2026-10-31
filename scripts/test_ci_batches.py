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
func TestNovember(t *testing.T) { sample(t); t.Run("TestAlpha", sample) }
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
        patterns = dict(re.findall(r"(GO_TEST_BATCH_[A-Z]+): '([^']+)'", workflow))
        commands = re.findall(
            r"python scripts/test-go.py -- -race -timeout=15m (-run|-skip) "
            r"'\$\{\{ env\.(GO_TEST_BATCH_[A-Z]+) \}\}' ./\.\.\.",
            workflow,
        )
        self.assertEqual(len(commands), 4)
        self.assertEqual(workflow.count("run: python scripts/test-go.py --"), len(commands))
        self.batches = [(flag, patterns[key]) for flag, key in commands]

    def batch(self, selection, failure=""):
        flag, pattern = selection
        with patch.dict(os.environ, BATCH_RECORD=str(self.record), BATCH_FAILURE=failure):
            code = guard.run([str(self.go), "-C", str(self.module), "test", "-race", "-count=1", "-timeout=30s",
                              flag, pattern, "./..."], self.parent, 120, 8 * guard.GIB, 4 * guard.GIB)
        self.assertEqual(list(self.parent.glob(guard.PREFIX + "*")), [])
        return code

    def test_union_runs_each_test_example_and_fuzz_seed_once(self):
        for selection in self.batches:
            self.assertEqual(self.batch(selection), 0)
        expected = ["TestAlpha", "TestAlpha/TestZulu", "TestMiddle", "TestNovember", "TestNovember/TestAlpha",
                    "TestZulu", "TestZulu/TestAlpha", "Test_Extra", "TestΩ", "FuzzAlpha/seed#0", "Example"]
        self.assertEqual(Counter(self.record.read_text(encoding="utf-8").splitlines()), Counter(expected))

    def test_failure_in_each_group_fails_its_batch(self):
        for selection, failure in zip(self.batches, ("TestAlpha", "TestMiddle", "TestNovember", "TestZulu")):
            with self.subTest(failure=failure):
                self.assertEqual(self.batch(selection, failure), 1)


if __name__ == "__main__":
    unittest.main()
