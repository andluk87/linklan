import importlib.util
from pathlib import Path
import struct
import unittest

spec = importlib.util.spec_from_file_location('resource', Path(__file__).with_name('build-resource.py'))
resource = importlib.util.module_from_spec(spec)
spec.loader.exec_module(resource)


class ResourceTests(unittest.TestCase):
    def setUp(self):
        self.ico = (Path(__file__).resolve().parent.parent / 'assets/linklan.ico').read_bytes()

    def test_icon_images_and_version_have_relocated_resource_entries(self):
        obj = resource.resource_object(self.ico, '1.2.3')
        machine, sections, timestamp, symbols, symbol_count, optional, flags = struct.unpack_from('<HHIIIHH', obj)
        self.assertEqual((machine, sections, timestamp, symbol_count), (0x8664, 1, 0, 1))
        name, _, _, size, raw, reloc, _, count, _, _ = struct.unpack_from('<8sIIIIIIHHI', obj, 20)
        self.assertEqual(name.rstrip(b'\0'), b'.rsrc')
        section = obj[raw:raw+size]
        self.assertEqual(struct.unpack_from('<H', section, 14)[0], 3)
        self.assertEqual([struct.unpack_from('<I', section, 16+i*8)[0] for i in range(3)], [3,14,16])
        self.assertEqual(count, struct.unpack_from('<H', self.ico, 4)[0]+2)
        for i in range(count):
            offset, symbol, kind = struct.unpack_from('<IIH', obj, reloc+i*10)
            self.assertEqual((symbol,kind), (0,3))
            rva, length, _, _ = struct.unpack_from('<IIII', section, offset)
            self.assertGreater(length, 0)
            self.assertLessEqual(rva+length, len(section))
        self.assertIn('LinkLAN'.encode('utf-16le'), section)
        self.assertIn('1.2.3'.encode('utf-16le'), section)
        self.assertEqual(obj, resource.resource_object(self.ico, '1.2.3'))

    def test_rejects_corrupt_icon_offsets(self):
        corrupt = bytearray(self.ico)
        struct.pack_into('<I', corrupt, 18, len(corrupt)+1)
        with self.assertRaises(ValueError):
            resource.resource_object(corrupt)


if __name__ == '__main__':
    unittest.main()
