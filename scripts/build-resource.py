#!/usr/bin/env python3
"""Compile the multi-resolution application ICO into a deterministic amd64 COFF resource.

Uses only Python's standard library, so Windows cross builds need no extra
compiler, network download or change to Go dependencies.
"""
from pathlib import Path
import struct

ROOT = Path(__file__).resolve().parent.parent


def version_info(version):
    parts = [int(p) for p in version.split('.')]
    if len(parts) != 3 or any(p < 0 or p > 65535 for p in parts):
        raise ValueError('Windows file version requires three components up to 65535')
    quad = (*parts, 0)

    def block(key, value=b'', children=(), text=False):
        body = bytearray(struct.pack('<HHH', 0, len(value) // 2 if text else len(value), int(text)))
        body.extend((key + '\0').encode('utf-16le'))
        body.extend(b'\0' * (-len(body) % 4))
        body.extend(value)
        for child in children:
            body.extend(b'\0' * (-len(body) % 4))
            body.extend(child)
        struct.pack_into('<H', body, 0, len(body))
        return bytes(body)

    fixed = struct.pack('<13I', 0xFEEF04BD, 0x10000, quad[0] << 16 | quad[1],
                        quad[2] << 16 | quad[3], quad[0] << 16 | quad[1],
                        quad[2] << 16 | quad[3], 0x3f, 0, 0x40004, 1, 0, 0, 0)
    strings = {'CompanyName': 'LinkLAN', 'FileDescription': 'LinkLAN — виртуальная локальная сеть',
               'FileVersion': version, 'InternalName': 'LinkLAN', 'OriginalFilename': 'LinkLAN.exe',
               'ProductName': 'LinkLAN', 'ProductVersion': version}
    table = block('040904B0', children=[block(k, (v + '\0').encode('utf-16le'), text=True) for k, v in strings.items()], text=True)
    return block('VS_VERSION_INFO', fixed, [block('StringFileInfo', children=[table], text=True),
                 block('VarFileInfo', children=[block('Translation', struct.pack('<HH', 0x409, 1200))])])


def resource_object(ico, version='0.3.0'):
    reserved, kind, count = struct.unpack_from('<HHH', ico)
    if reserved or kind != 1 or not 1 <= count <= 32:
        raise ValueError('Expected a Windows ICO file with 1..32 images')
    entries = []
    for i in range(count):
        w, h, colors, reserved, planes, bits, size, offset = struct.unpack_from('<BBBBHHII', ico, 6 + i * 16)
        if reserved or offset < 6 + 16 * count or size == 0 or offset + size > len(ico):
            raise ValueError('Invalid ICO image bounds')
        entries.append((w, h, colors, planes, bits, ico[offset:offset + size]))
    group = struct.pack('<HHH', 0, 1, count)
    for i, (w, h, colors, planes, bits, data) in enumerate(entries):
        group += struct.pack('<BBBBHHIH', w, h, colors, 0, planes, bits, len(data), i + 1)
    # Resource tree: type -> numeric ID -> en-US language -> data entry.
    data = bytearray()

    def directory(ids):
        offset = len(data)
        data.extend(struct.pack('<IIHHHH', 0, 0, 0, 0, 0, len(ids)))
        patches = []
        for resource_id in ids:
            data.extend(struct.pack('<II', resource_id, 0))
            patches.append(len(data) - 4)
        return offset, patches

    _, root = directory([3, 14, 16])
    icons, ids = directory(range(1, count + 1))
    struct.pack_into('<I', data, root[0], icons | 0x80000000)
    groups, group_ids = directory([1])
    struct.pack_into('<I', data, root[1], groups | 0x80000000)
    versions, version_ids = directory([1])
    struct.pack_into('<I', data, root[2], versions | 0x80000000)
    leaves = []
    for patch, payload in list(zip(ids, [e[5] for e in entries])) + [(group_ids[0], group), (version_ids[0], version_info(version))]:
        language, language_patches = directory([0x409])
        struct.pack_into('<I', data, patch, language | 0x80000000)
        leaves.append((language_patches[0], payload))
    records = []
    for patch, payload in leaves:
        entry = len(data)
        data.extend(struct.pack('<IIII', 0, len(payload), 0, 0))
        struct.pack_into('<I', data, patch, entry)
        records.append((entry, payload))
    relocations = bytearray()
    for entry, payload in records:
        data.extend(b'\0' * (-len(data) % 4))
        struct.pack_into('<I', data, entry, len(data))
        data.extend(payload)
        # IMAGE_REL_AMD64_ADDR32NB: image-relative RVA of the .rsrc symbol.
        relocations.extend(struct.pack('<IIH', entry, 0, 3))
    raw_offset = 20 + 40
    reloc_offset = raw_offset + len(data)
    symbols_offset = reloc_offset + len(relocations)
    header = struct.pack('<HHIIIHH', 0x8664, 1, 0, symbols_offset, 1, 0, 0)
    section = struct.pack('<8sIIIIIIHHI', b'.rsrc', 0, 0, len(data), raw_offset,
                          reloc_offset, 0, len(records), 0, 0x40000040)
    symbol = struct.pack('<8sIhHBB', b'.rsrc', 0, 1, 0, 3, 0)
    return header + section + data + relocations + symbol + struct.pack('<I', 4)


if __name__ == '__main__':
    target = ROOT / 'cmd/client/resource_windows_amd64.syso'
    target.write_bytes(resource_object((ROOT / 'assets/linklan.ico').read_bytes(), (ROOT / 'release-version').read_text().strip()))
    print('Compiled Windows application icon:', target.name)
