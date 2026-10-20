import importlib.util
from pathlib import Path
import tempfile
import unittest
from unittest import mock


spec = importlib.util.spec_from_file_location('web_transfer', Path(__file__).with_name('run.py'))
transfer = importlib.util.module_from_spec(spec)
spec.loader.exec_module(transfer)


class PayloadTests(unittest.TestCase):
    def test_default_payload_remains_random(self):
        with mock.patch.object(transfer.secrets, 'token_bytes', return_value=b'random') as random:
            self.assertEqual(transfer.load_payload(8), b'random')
        random.assert_called_once_with(8 << 20)

    def test_matched_payload_reuses_exact_bytes(self):
        with tempfile.TemporaryDirectory() as directory:
            path = Path(directory) / 'fixture.bin'
            expected = bytes(range(256)) * ((8 << 20) // 256)
            path.write_bytes(expected)
            self.assertEqual(transfer.load_payload(8, path), expected)
            self.assertEqual(transfer.load_payload(8, path), expected)

    def test_rejects_size_mismatch_and_empty_file(self):
        with tempfile.TemporaryDirectory() as directory:
            path = Path(directory) / 'fixture.bin'
            for size in (0, (1 << 20) - 1, (1 << 20) + 1, 2 << 20):
                with self.subTest(size=size):
                    path.write_bytes(b'x' * size)
                    with self.assertRaises(ValueError):
                        transfer.load_payload(1, path)

    def test_rejects_out_of_range_before_allocation(self):
        with mock.patch.object(transfer.secrets, 'token_bytes') as random:
            for size in (0, -1, 65):
                with self.subTest(size=size), self.assertRaises(ValueError):
                    transfer.load_payload(size)
            random.assert_not_called()

    def test_rejects_missing_file_and_directory(self):
        with tempfile.TemporaryDirectory() as directory:
            for path in (Path(directory), Path(directory) / 'missing'):
                with self.subTest(path=path), self.assertRaises(ValueError):
                    transfer.load_payload(1, path)


if __name__ == '__main__':
    unittest.main()
