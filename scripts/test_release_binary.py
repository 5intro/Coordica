"""Release target checks use file contents, never the host or input filename."""
import contextlib
import importlib.util
import io
from pathlib import Path
import struct
import sys
import tempfile
import unittest
from unittest import mock

spec = importlib.util.spec_from_file_location('package_release', Path(__file__).with_name('package-release.py'))
release = importlib.util.module_from_spec(spec)
spec.loader.exec_module(release)


def binary(platform):
    if platform == 'darwin-arm64':
        data = bytearray(128)
        struct.pack_into('<8I', data, 0, 0xFEEDFACF, 0x0100000C, 0, 2, 1, 72, 0, 0)
        struct.pack_into('<2I16s4Q4I', data, 32, 0x19, 72, b'__TEXT',
                         0x100000000, 128, 0, 128, 7, 5, 0, 0)
    elif platform == 'linux-amd64':
        data = bytearray(192)
        data[:7] = b'\x7fELF\x02\x01\x01'
        struct.pack_into('<HHIQQQIHHHHHH', data, 16, 2, 62, 1, 0x400000,
                         64, 0, 0, 64, 56, 1, 64, 0, 0)
        struct.pack_into('<IIQQQQQQ', data, 64, 1, 5, 0, 0x400000, 0, 192, 192, 4096)
    else:
        data = bytearray(512)
        data[:2] = b'MZ'
        struct.pack_into('<I', data, 60, 64)
        data[64:68] = b'PE\0\0'
        struct.pack_into('<HHIIIHH', data, 68, 0x8664, 1, 0, 0, 0, 112, 2)
        struct.pack_into('<H', data, 88, 0x20B)
        struct.pack_into('<I', data, 148, 496)
        struct.pack_into('<II', data, 216, 16, 496)
    return bytes(data)


class BinaryTests(unittest.TestCase):
    def test_targets_and_misleading_names(self):
        for platform, source, _, _ in release.PLATFORMS:
            with self.subTest(platform=platform):
                release.validate_binary(binary(platform), platform, source)
                release.validate_binary(binary(platform), platform, 'misleading.exe')
                for other, _, _, _ in release.PLATFORMS:
                    if other != platform:
                        with self.assertRaisesRegex(ValueError, f'for {other}: expected {other}'):
                            release.validate_binary(binary(platform), other, source)

    def test_every_truncated_prefix_rejected(self):
        for platform, source, _, _ in release.PLATFORMS:
            data = binary(platform)
            for end in range(len(data)):
                with self.subTest(platform=platform, end=end), self.assertRaises(ValueError):
                    release.validate_binary(data[:end], platform, source)

    def test_invalid_headers_architecture_and_ranges(self):
        mutations = {
            'darwin-arm64': [(4, '<I', 0x01000007), (12, '<I', 6), (16, '<I', 0),
                             (20, '<I', 1000), (36, '<I', 7), (36, '<I', 80),
                             (72, '<Q', 1000), (80, '<Q', 1000), (96, '<I', 1)],
            'linux-amd64': [(4, '<B', 1), (5, '<B', 2), (6, '<B', 0),
                            (7, '<B', 9), (16, '<H', 1), (18, '<H', 183), (20, '<I', 0),
                            (32, '<Q', 1000), (52, '<H', 0), (54, '<H', 0),
                            (56, '<H', 0), (64, '<I', 0), (72, '<Q', 1000),
                            (96, '<Q', 1000), (60, '<H', 1)],
            'windows-amd64': [(60, '<I', 0), (60, '<I', 1000), (64, '<I', 0),
                              (68, '<H', 0xAA64), (70, '<H', 0), (84, '<H', 111),
                              (84, '<H', 1000), (86, '<H', 0), (86, '<H', 0x2002),
                              (88, '<H', 0x10B), (148, '<I', 0), (148, '<I', 1000),
                              (196, '<I', 1), (216, '<I', 1000), (220, '<I', 1000)],
        }
        for platform, changes in mutations.items():
            for offset, fmt, value in changes:
                for invalid in (value, (1 << (8 * struct.calcsize(fmt))) - 1):
                    data = bytearray(binary(platform))
                    struct.pack_into(fmt, data, offset, invalid)
                    with self.subTest(platform=platform, offset=offset, value=invalid), self.assertRaises(ValueError):
                        release.validate_binary(data, platform, 'coordica')
        for data in (b'', b'not a binary', b'#!/bin/sh\necho hello', b'\xca\xfe\xba\xbe' + bytes(128)):
            with self.subTest(data=data), self.assertRaisesRegex(ValueError, 'unrecognized executable'):
                release.validate_binary(data, 'darwin-arm64', 'coordica')

    def test_macho_section_and_relocation_ranges(self):
        data = bytearray(binary('darwin-arm64')) + bytearray(80)
        struct.pack_into('<I', data, 20, 152)
        struct.pack_into('<I', data, 36, 152)
        struct.pack_into('<Q', data, 80, 208)
        struct.pack_into('<I', data, 96, 1)
        struct.pack_into('<QI', data, 144, 24, 184)
        release.validate_binary(data, 'darwin-arm64', 'coordica')
        for offset, fmt in ((144, '<Q'), (152, '<I'), (160, '<I'), (164, '<I')):
            bad = bytearray(data)
            struct.pack_into(fmt, bad, offset, (1 << (8 * struct.calcsize(fmt))) - 1)
            with self.subTest(offset=offset), self.assertRaises(ValueError):
                release.validate_binary(bad, 'darwin-arm64', 'coordica')
        for kind in (1, 0xC, 0x12):
            zerofill = bytearray(data)
            struct.pack_into('<Q', zerofill, 144, 4096)
            struct.pack_into('<I', zerofill, 168, kind)
            release.validate_binary(zerofill, 'darwin-arm64', 'coordica')

    def test_elf_pie_and_nobits_sections(self):
        data = bytearray(binary('linux-amd64'))
        struct.pack_into('<H', data, 16, 3)  # ET_DYN for PIE.
        struct.pack_into('<Q', data, 40, 128)
        struct.pack_into('<H', data, 60, 1)
        struct.pack_into('<IIQQQQIIQQ', data, 128, 0, 8, 0, 0, 192, 4096, 0, 0, 0, 0)
        release.validate_binary(data, 'linux-amd64', 'coordica')
        struct.pack_into('<I', data, 132, 1)  # PROGBITS must be in the file.
        with self.assertRaises(ValueError):
            release.validate_binary(data, 'linux-amd64', 'coordica')

    def test_main_preflights_all_targets_before_touching_outputs(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            (root / 'bin').mkdir()
            with mock.patch.object(release, 'ROOT', root), \
                    mock.patch.object(release, 'common_entries', return_value={'README.md': (b'readme', 0o644), 'examples/dsh/coordica.cordis.yml': (b'overlay', 0o644)}), \
                    mock.patch.object(sys, 'argv', ['package-release.py', '--version', '1.2.3']):
                for bad_platform, _, _, _ in release.PLATFORMS:
                    for platform, source, _, _ in release.PLATFORMS:
                        (root / 'bin' / source).write_bytes(binary(platform) if platform != bad_platform else b'bad')
                    outputs = [root / 'bin' / f'coordica-1.2.3-{p}{ext}' for p, _, _, ext in release.PLATFORMS]
                    outputs.append(root / 'bin' / 'SHA256SUMS')
                    for output in outputs:
                        output.write_bytes(b'previous release')
                    with self.subTest(platform=bad_platform), self.assertRaisesRegex(ValueError, bad_platform):
                        release.main()
                    for output in outputs:
                        self.assertEqual(output.read_bytes(), b'previous release')
                    self.assertFalse(list((root / 'bin').glob('.coordica-release-*')))
                for platform, source, _, _ in release.PLATFORMS:
                    (root / 'bin' / source).write_bytes(binary(platform))
                with contextlib.redirect_stdout(io.StringIO()):
                    release.main()
                self.assertEqual(len((root / 'bin' / 'SHA256SUMS').read_text().splitlines()), 3)
                for platform, _, name, ext in release.PLATFORMS:
                    entries = {'README.md': (b'readme', 0o644), 'examples/dsh/coordica.cordis.yml': (b'overlay', 0o644), name: (binary(platform), 0o755),
                               'START-HERE.txt': (release.start_here('1.2.3', platform, name), 0o644)}
                    if platform == 'windows-amd64':
                        entries['coordica.cordis.yml'] = (b'overlay', 0o644)
                    release.verify_archive(root / 'bin' / f'coordica-1.2.3-{platform}{ext}', entries)


if __name__ == '__main__':
    unittest.main()
