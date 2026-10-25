"""Bounded executable identity checks; no I/O, signatures or loader emulation."""
import struct


class _BinaryLayout:
    def __init__(self, data, platform, source):
        self.data = data
        self.platform = platform
        self.source = source

    def fail(self, reason):
        raise ValueError(f"invalid release binary bin/{self.source} for {self.platform}: {reason}; "
                         "rebuild the binary for the intended GOOS/GOARCH")

    def require(self, condition, reason):
        if not condition:
            self.fail(reason)

    def span(self, offset, size):
        self.require(0 <= offset <= len(self.data) and 0 <= size <= len(self.data) - offset,
                     "truncated executable or invalid file range")

    def unpack(self, fmt, offset):
        self.span(offset, struct.calcsize(fmt))
        return struct.unpack_from(fmt, self.data, offset)


def validate_binary(data, platform, source):
    """Check target identity/header bounds, not full runtime validity or signatures."""
    layout = _BinaryLayout(data, platform, source)
    if data.startswith(b'\xcf\xfa\xed\xfe'):
        _validate_macho(layout)
    elif data.startswith(b'\x7fELF'):
        _validate_elf(layout)
    elif data.startswith(b'MZ'):
        _validate_pe(layout)
    else:
        layout.fail("unrecognized executable format (expected Mach-O arm64, ELF amd64 or PE amd64)")


def _validate_macho(layout):
    data, platform = layout.data, layout.platform
    unpack, require, span = layout.unpack, layout.require, layout.span
    _, cpu, _, kind, count, size, _, _ = unpack('<8I', 0)
    require(platform == 'darwin-arm64' and cpu == 0x0100000C,
            f"expected {platform}, found Mach-O CPU 0x{cpu:08x}")
    require(kind == 2, "Mach-O is not an executable")
    span(32, size)
    require(count > 0 and count <= size // 8, "invalid Mach-O load commands")
    offset, end, segments = 32, 32 + size, 0
    for _ in range(count):
        command, length = unpack('<2I', offset)
        require(length >= 8 and length % 8 == 0 and length <= end - offset,
                "invalid Mach-O load command size")
        if command == 0x19:  # LC_SEGMENT_64
            require(length >= 72, "truncated Mach-O segment command")
            file_offset, file_size = unpack('<2Q', offset + 40)
            span(file_offset, file_size)
            sections, = unpack('<I', offset + 64)
            require(length == 72 + 80 * sections, "invalid Mach-O section table")
            for index in range(sections):
                section = offset + 72 + 80 * index
                section_size, section_offset = unpack('<QI', section + 40)
                relocation_offset, relocations, flags = unpack('<3I', section + 56)
                if flags & 0xFF not in (1, 0xC, 0x12):  # Zero-fill section types.
                    span(section_offset, section_size)
                span(relocation_offset, relocations * 8)
            segments += 1
        offset += length
    require(offset == end and segments > 0, "invalid Mach-O segment table")


def _validate_elf(layout):
    data, platform = layout.data, layout.platform
    unpack, require, span = layout.unpack, layout.require, layout.span
    span(0, 64)
    require(data[4:7] == b'\x02\x01\x01', "expected little-endian ELF64 version 1")
    require(data[7] in (0, 3), "ELF OS ABI is not System V/Linux")
    kind, machine, version, _, phoff, shoff, _, ehsize, phsize, phnum, shsize, shnum, _ = unpack('<HHIQQQIHHHHHH', 16)
    require(platform == 'linux-amd64' and machine == 62,
            f"expected {platform}, found ELF machine {machine}")
    require(kind in (2, 3) and version == 1 and ehsize == 64,
            "invalid ELF executable header")
    require(phnum > 0 and phsize == 56, "invalid ELF program header table")
    span(phoff, phsize * phnum)
    loads = 0
    for index in range(phnum):
        kind, _, offset, _, _, size, _, _ = unpack('<IIQQQQQQ', phoff + index * phsize)
        span(offset, size)
        loads += kind == 1  # PT_LOAD
    require(loads > 0, "ELF has no loadable segments")
    if shnum:
        require(shsize == 64, "invalid ELF section header size")
        span(shoff, shsize * shnum)
        for index in range(shnum):
            _, kind, _, _, offset, size, _, _, _, _ = unpack('<IIQQQQIIQQ', shoff + index * shsize)
            if kind != 8:  # SHT_NOBITS occupies no file bytes.
                span(offset, size)


def _validate_pe(layout):
    data, platform = layout.data, layout.platform
    unpack, require, span = layout.unpack, layout.require, layout.span
    span(0, 64)
    pe, = unpack('<I', 60)
    require(pe >= 64, "invalid PE header offset")
    span(pe, 24)
    require(data[pe:pe + 4] == b'PE\0\0', "invalid PE signature")
    machine, count, _, _, _, optional_size, flags = unpack('<HHIIIHH', pe + 4)
    require(platform == 'windows-amd64' and machine == 0x8664,
            f"expected {platform}, found PE machine 0x{machine:04x}")
    require(flags & 2 and not flags & 0x2000, "PE is not an executable application")
    require(optional_size >= 112, "truncated PE optional header")
    span(pe + 24, optional_size)
    magic, = unpack('<H', pe + 24)
    require(magic == 0x20B, "expected PE32+ optional header")
    directory_count, = unpack('<I', pe + 24 + 108)
    require(112 + 8 * directory_count <= optional_size, "truncated PE data directories")
    table = pe + 24 + optional_size
    require(count > 0, "PE has no sections")
    span(table, 40 * count)
    header_size, = unpack('<I', pe + 24 + 60)
    require(header_size >= table + 40 * count, "invalid PE header size")
    span(0, header_size)
    for index in range(count):
        size, offset = unpack('<II', table + index * 40 + 16)
        span(offset, size)

