import asyncio
import importlib.util
from pathlib import Path
import tempfile
from types import SimpleNamespace
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


class StartupTests(unittest.IsolatedAsyncioTestCase):
    async def startup(self, carrier='https', missing_headers=False, stalled_headers=False,
                      extra_stalled_response=False, late_failure=False, header_delay=0):
        bridge = transfer.BrowserBridge({'url': 'https://example.com/bridge/',
                                         'raw_secret': '00' * 16, 'carrier': 'https'})

        async def noop(*args, **kwargs):
            pass

        class Response:
            url = bridge.session_url
            request = SimpleNamespace(method='POST')

            def __init__(self, stalled=False):
                self.stalled = stalled

            async def all_headers(self):
                if self.stalled:
                    await asyncio.Future()
                if header_delay:
                    await asyncio.sleep(header_delay)
                if late_failure:
                    bridge.fail('synthetic late failure')
                return {'x-session-token': 'synthetic', 'x-carrier-mode': carrier}

        class Page:
            evaluate = mock.AsyncMock()

            def on(self, *args):
                pass

            async def goto(self, *args, **kwargs):
                bridge.ready.set()
                if not missing_headers:
                    asyncio.get_running_loop().call_later(0.01, bridge.response, Response(stalled_headers))
                    if extra_stalled_response:
                        asyncio.get_running_loop().call_later(0.01, bridge.response, Response(True))

        context = SimpleNamespace(route=noop, new_page=mock.AsyncMock(return_value=Page()))
        browser = SimpleNamespace(new_context=mock.AsyncMock(return_value=context), close=mock.AsyncMock())
        playwright = SimpleNamespace(chromium=SimpleNamespace(launch=mock.AsyncMock(return_value=browser)), stop=mock.AsyncMock())
        runner = SimpleNamespace(setup=noop, addresses=[('127.0.0.1', 12345)], cleanup=mock.AsyncMock())
        listener = SimpleNamespace(sockets=[SimpleNamespace(getsockname=lambda: ('127.0.0.1', 23456))])
        http = mock.MagicMock()
        http.__aenter__.return_value = http
        http.delete.return_value.__aenter__.return_value.status = 204
        original_timeout = asyncio.timeout
        with mock.patch.object(transfer.web, 'AppRunner', return_value=runner), \
                mock.patch.object(transfer.web, 'TCPSite', return_value=SimpleNamespace(start=noop)), \
                mock.patch.object(transfer, 'async_playwright', return_value=SimpleNamespace(start=mock.AsyncMock(return_value=playwright))), \
                mock.patch.object(transfer, 'ClientSession', return_value=http), \
                mock.patch.object(transfer, 'RESPONSE_DRAIN_TIMEOUT', 0.02), \
                mock.patch.object(transfer.asyncio, 'start_server', new_callable=mock.AsyncMock, return_value=listener) as start_server, \
                mock.patch.object(transfer.asyncio, 'timeout', side_effect=lambda seconds: original_timeout(min(seconds, 0.05))) as deadline:
            try:
                await bridge.__aenter__()
            except BaseException:
                browser.close.assert_awaited_once()
                playwright.stop.assert_awaited_once()
                runner.cleanup.assert_awaited_once()
                self.assertTrue(all(task.done() for task in bridge.response_tasks))
                if header_delay:
                    self.assertEqual(bridge.tokens, {'synthetic'})
                if bridge.tokens:
                    self.assertTrue(bridge.stats['cleanup_ok'])
                    self.assertEqual(bridge.stats['sessions_deleted'], len(bridge.tokens))
                    http.delete.assert_called_once()
                start_server.assert_not_awaited()
                deadline.assert_called_once_with(30)
                raise
            self.assertEqual(bridge.stats['carrier'], 'https')
            self.assertTrue(bridge.session_metadata.is_set())
            self.assertEqual(bridge.stats['sessions_created'], 1)
            start_server.assert_awaited_once()
            deadline.assert_called_once_with(30)
            browser.close.assert_not_awaited()
        return bridge

    async def test_welcome_before_response_metadata_waits_for_headers(self):
        bridge = await self.startup()
        self.assertEqual(bridge.tokens, {'synthetic'})

    async def test_mismatched_carrier_still_rejected_and_cleaned_up(self):
        with self.assertRaisesRegex(ValueError, 'trial carrier changed'):
            await self.startup(carrier='websocket')

    async def test_missing_response_metadata_has_bounded_cleanup(self):
        with self.assertRaises(TimeoutError):
            await self.startup(missing_headers=True)

    async def test_stalled_header_observer_cannot_block_browser_cleanup(self):
        async with asyncio.timeout(0.5):
            with self.assertRaises(TimeoutError):
                await self.startup(stalled_headers=True)

    async def test_observer_gather_shares_startup_deadline(self):
        async with asyncio.timeout(0.5):
            with self.assertRaises(TimeoutError):
                await self.startup(extra_stalled_response=True)

    async def test_failure_during_header_capture_prevents_listener(self):
        with self.assertRaisesRegex(ConnectionError, 'synthetic late failure'):
            await self.startup(late_failure=True)

    async def test_cleanup_captures_late_available_token_for_delete(self):
        with self.assertRaises(TimeoutError):
            await self.startup(header_delay=0.045)


if __name__ == '__main__':
    unittest.main()
