"""Public release wording and archive safety regression checks."""
import importlib.util
from pathlib import Path
import tempfile
import unittest

spec = importlib.util.spec_from_file_location('package_release', Path(__file__).with_name('package-release.py'))
release = importlib.util.module_from_spec(spec)
spec.loader.exec_module(release)


class ReleaseTests(unittest.TestCase):
    def test_phase_b_start_here_for_every_platform(self):
        for platform, _, binary, _ in release.PLATFORMS:
            with self.subTest(platform=platform):
                text = release.start_here('0.2.3-dev', platform, binary).decode()
                for expected in ('Coordica v0.2.3-dev', '/releases/tag/v0.2.3-dev',
                                 'gpt-6.1-sol', '默认关闭', '测试不替代授权',
                                 'macOS 本机 Phase B 端到端验收已通过', '进程树停止证据',
                                 '原生 Leader 运行中取消验收也已通过',
                                 '结构化会话', '真实存活 PID', '保留实际耗时支出',
                                 'Owner 仅批准目标一次', '没有手动批准范围',
                                 '所有 Run 进程树均已停止', '项目与目标预留预算均为零',
                                 '同一任务以新授权恢复并保留累计支出',
                                 'node:test', '本地测试 Git 的 main',
                                 '不代表完整 Phase B dsh 协作或多机部署已通过',
                                 '当前发布版本关联的 GitHub checks',
                                 '多机 Phase B 与 Linux 原生部署仍待验证',
                                 '运行中取消的 Phase A 验收已通过', 'Claude / dsh',
                                 'coordica-chat', '--interaction', '唯一的 coordica', '--runner-data', '稳定绝对路径',
                                 '完整 macOS Go race 套件', '包括全部监听测试', '隔离双监听中心',
                                 '十一项中心工具', '七项工具', '没有运行模型', '没有激活用户服务',
                                 'GUI 动态工具刷新取决于客户端支持', 'fixtures 不代表',
                                 '重新加载或重启', '七天', '365 天', '旧 budget 阻断不自动迁移',
                                 '忽略的弃用字段', '尚未实现', 'Owner 独立验证后部署与发布'):
                    self.assertIn(expected, text)
                self.assertNotIn('端到端验证正在进行', text)
                self.assertNotIn('取消与断线等故障场景仍待实测', text)

    def test_public_execution_intent_boundary(self):
        documents = {'README.md': (release.ROOT / 'README.md').read_text()}
        for platform, _, binary, _ in release.PLATFORMS:
            documents[platform] = release.start_here('0.2.3-dev', platform, binary).decode()
        for name, text in documents.items():
            with self.subTest(document=name):
                for expected in ('native execution intent v1', 'requested_paths',
                                 'scope_requested', 'blocked', 'completed',
                                 '不是完成证据', '意图与请求回执均不授予权限',
                                 '任何编辑前停止', '固定工作区无变化',
                                 '重大工作不会自动批准', '累计支出',
                                 '规划重试', '进程树停止'):
                    self.assertIn(expected, text)
                self.assertNotIn('端到端验证正在进行', text)

    def test_private_inputs_rejected(self):
        for name in ('docs/private.md', 'owner.token', '.aws/config',
                     '../README.md', 'center.key', 'node_modules/file',
                     'runner.json', 'discovery.json', 'state.sqlite3',
                     '.env.local', '.ssh/id_ed25519', 'credentials.json',
                     'host.json.init.pending', 'host.json.init.lock',
                     'host.json.write.lock', 'coordica-chat.json', 'interaction.json',
                     '.chat-profile-pending-private'):
            with self.subTest(name=name), self.assertRaises(ValueError):
                release.checked_name(name)

    def test_public_package_inventory(self):
        entries = release.common_entries()
        expected = {
            'LICENSE', 'THIRD_PARTY_NOTICES.md', 'README.md', 'go.mod', 'go.sum',
            'internal/process/README.md', 'examples/dsh/coordica.cordis.yml',
            'examples/tools/custom-acp.json', 'examples/acceptance/README.md',
            'examples/acceptance/.gitignore', 'examples/acceptance/src/greeting.mjs',
            'examples/acceptance/test/greeting.test.mjs',
            'examples/acceptance/tools/verify-acceptance.mjs',
            'examples/acceptance/task.json',
            'examples/native-scope/README.md', 'examples/native-scope/goal.json',
            'examples/native-scope/task.json', 'examples/native-scope/flow.json',
            'examples/native-scope/expected-scope-intent.json',
            'examples/native-scope/src/greeting.mjs',
            'examples/native-scope/test/greeting.test.mjs',
            'examples/native-scope/tools/verify-acceptance.mjs',
            'examples/testing/README.md',
        }
        self.assertEqual({name for name in entries if not name.startswith('third_party/licenses/')}, expected)
        for name in entries:
            self.assertEqual(release.checked_name(name), name)
        self.assertIn('个人非商业使用许可协议', entries['LICENSE'][0].decode())

    def test_archive_round_trip_and_determinism(self):
        entries = release.common_entries()
        entries['coordica'] = (b'test binary', 0o755)
        with tempfile.TemporaryDirectory() as directory:
            for suffix, writer in (('.tar.gz', release.write_tar), ('.zip', release.write_zip)):
                first, second = (Path(directory) / (name + suffix) for name in ('first', 'second'))
                writer(first, entries, 0)
                writer(second, entries, 0)
                release.verify_archive(first, entries)
                self.assertEqual(first.read_bytes(), second.read_bytes())


if __name__ == '__main__':
    unittest.main()
