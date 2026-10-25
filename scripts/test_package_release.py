"""Public release wording and archive safety regression checks."""
import hashlib
import importlib.util
from pathlib import Path
import re
import tempfile
import unittest

spec = importlib.util.spec_from_file_location('package_release', Path(__file__).with_name('package-release.py'))
release = importlib.util.module_from_spec(spec)
spec.loader.exec_module(release)


class ReleaseTests(unittest.TestCase):
    def assert_readme_references(self, text, readme):
        references = re.findall(r'“([^”]+)”（#([a-z0-9-]+)）', text)
        self.assertTrue(references, 'START-HERE has no anchored README references')
        for title, anchor in references:
            marker = f'<a id="{anchor}"></a>'
            self.assertIn(marker, readme, f'Missing README anchor: {anchor}')
            section = readme.split(marker, 1)[1].split('<a id="', 1)[0]
            headings = re.findall(r'^#{1,6} (.+)$', section, re.MULTILINE)
            headings += re.findall(r'<summary><strong>([^<]+)</strong></summary>', section)
            self.assertIn(title, headings, f'Wrong README title at #{anchor}: {title}')

    def test_start_here_readme_references_for_every_platform(self):
        readme = (release.ROOT / 'README.md').read_text(encoding='utf-8')
        for platform, _, binary, _ in release.PLATFORMS:
            with self.subTest(platform=platform):
                text = release.start_here('0.2.5-dev', platform, binary).decode()
                self.assert_readme_references(text, readme)

    def test_readme_references_reject_missing_anchor_or_wrong_section(self):
        text = 'README.md 的“开始使用”（#getting-started）。'
        valid = '<a id="getting-started"></a>\n## 开始使用\n'
        self.assert_readme_references(text, valid)
        invalid = (
            valid.replace('getting-started', 'renamed'),
            valid.replace('## 开始使用', '## 新标题'),
            '<a id="other"></a>\n## 开始使用\n'
            '<a id="getting-started"></a>\n## 新标题\n',
            valid.replace('## 开始使用', '正文提及开始使用，不是章节标题'),
        )
        for readme in invalid:
            with self.subTest(readme=readme), self.assertRaises(AssertionError):
                self.assert_readme_references(text, readme)

    def test_readme_references_require_anchored_navigation(self):
        with self.assertRaises(AssertionError):
            self.assert_readme_references('README.md 的“旧标题”', '## 旧标题')

    def test_start_here_usage_and_safety_for_every_platform(self):
        for platform, _, binary, _ in release.PLATFORMS:
            with self.subTest(platform=platform):
                text = release.start_here('0.2.5-dev', platform, binary).decode()
                for expected in ('Coordica v0.2.5-dev', '/releases/tag/v0.2.5-dev',
                                 '源码与发布包可能对应不同提交', 'gpt-6.1-sol',
                                 '默认关闭', '测试不替代授权', '进程树停止证据',
                                 '个人非商业使用', '不是 OSI 批准的开源许可',
                                 'setup 预览', 'setup --apply', '已安装并登录',
                                 'HTTPS', '公共 CA', 'TLS 证书 SAN',
                                 'Owner token 不经 MCP 返回', '不可放入源码或公开包',
                                 '解压不会自动安装或启动系统服务', '稳定绝对路径',
                                 '冲突保留', '旧无范围凭据须显式 --renew',
                                 '重试使用原内容与同一键', '恢复不清零支出',
                                 '不绕过重试上限', '重大决定仍由 Owner 处理'):
                    self.assertIn(expected, text)
                # Reject internal progress reports, not words needed for honest limitations.
                for internal_status in ('后续功能',
                                        '验收已通过', '验收通过', 'Phase A', 'Phase B',
                                        'fixtures', '真实存活 PID', 'macOS Go race 套件'):
                    self.assertNotIn(internal_status, text)

    def test_start_here_rendering_preserves_exact_prior_guide(self):
        expected = {
            'darwin-arm64': '29073f513929e58d18ce7f53bf732bb782bcd827986f30b28648c2b7c2d7021d',
            'windows-amd64': 'c52a3d5ed74f1d49f316ca0e89bc9cde5df8f362e0b6e5f2e3f04891578da7e0',
            'linux-amd64': '2333226fa56f258ff170b3dc2f0f99864ce4361b7ce6e5df999555ee586144aa',
        }
        for platform, _, binary, _ in release.PLATFORMS:
            with self.subTest(platform=platform):
                text = release.start_here('0.2.5-dev', platform, binary)
                self.assertEqual(hashlib.sha256(text).hexdigest(), expected[platform])

    def test_hosted_setup_and_budget_recovery_documented(self):
        readme = (release.ROOT / 'README.md').read_text(encoding='utf-8')
        setup = readme.split('<a id="setup-reference"></a>', 1)[1].split('</details>', 1)[0]
        for expected in ('仅同机使用', '需要跨机 Runner',
                         '--console-listen 127.0.0.1:8080',
                         '--runner-listen 192.168.1.10:8443',
                         '--runner-url https://192.168.1.10:8443',
                         '不覆盖已有不同配置', '--github-token-file',
                         '--git-auth-file', '--git-auth-remote',
                         '不加 `Bearer` 前缀', 'Authorization:', '0600'):
            self.assertIn(expected, setup)
        operations = readme.split('<a id="operations-reference"></a>', 1)[1].split('</details>', 1)[0]
        for expected in ('旧版 budget 阻断', '预留支出为零', 'expected_version',
                         '/api/v1/tasks/<task-id>/resume', '相同幂等键',
                         '保留累计支出', '已有发布意图'):
            self.assertIn(expected, operations)

    def test_readme_local_links_are_in_package(self):
        entries = release.common_entries()
        readme = entries['README.md'][0].decode()
        links = re.findall(r'\]\(([^)]+)\)', readme)
        links += re.findall(r'<img[^>]+src="([^"]+)"', readme)
        for link in links:
            if link.startswith(('https://', 'http://', '#')):
                continue
            target = link.split('#', 1)[0]
            self.assertIn(target, entries, f'Broken packaged README link: {link}')

    def test_public_execution_intent_boundary(self):
        documents = {'README.md': (release.ROOT / 'README.md').read_text(encoding='utf-8')}
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
            'cmd/coordica/main.go', 'api/v1.schema.json', 'web/public/favicon.svg',
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
