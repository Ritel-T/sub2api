import copy
import hashlib
import hmac
import json
import unittest
from unittest.mock import patch

from bridge import Bridge, admitted

EVENT = {"self_id": 12345678, "user_id": 87654321, "time": 1000, "message_id": 7,
         "post_type": "message", "message_type": "private", "sub_type": "friend",
         "message": [{"type": "text", "data": {"text": "/sub 余额"}}]}


class BridgeTests(unittest.TestCase):
    def test_admission(self):
        self.assertTrue(admitted(EVENT, "12345678", 1000))
        for key, value in [("self_id", 111111), ("user_id", True), ("sub_type", "group"),
                           ("message_type", "group"), ("post_type", "notice"), ("time", 0),
                           ("time", 2000), ("time", "1000"), ("message", "CQ:text")]:
            with self.subTest(key=key, value=value):
                self.assertFalse(admitted({**EVENT, key: value}, "12345678", 1000))

    def test_plain_commands_only(self):
        for text in ["hello", "/submarine", "/SUB 余额", "[CQ:at,qq=all]", "/sub " + "x" * 257]:
            ev = copy.deepcopy(EVENT)
            ev["message"][0]["data"]["text"] = text
            self.assertFalse(admitted(ev, "12345678", 1000))
        ev = copy.deepcopy(EVENT)
        ev["message"].append({"type": "image", "data": {}})
        self.assertFalse(admitted(ev, "12345678", 1000))

    def bridge(self):
        b = Bridge.__new__(Bridge)
        b.bot, b.secret, b.webhook = "12345678", b"x" * 32, "https://example.test/api/v1/qq/webhook"
        return b

    def test_signing_exact_utf8_and_minimum_data(self):
        b = self.bridge()
        with patch("bridge.time.time", return_value=1000), patch.object(b, "is_friend", return_value=True), patch("bridge.post") as send:
            b.forward({**EVENT, "sender": {"nickname": "private"}})
        url, raw, headers = send.call_args.args
        self.assertNotIn("sender", json.loads(raw))
        self.assertEqual(headers["X-QQ-Signature"], "sha256=" + hmac.new(b.secret, raw, hashlib.sha256).hexdigest())

    def test_nonfriend_not_forwarded(self):
        b = self.bridge()
        with patch("bridge.time.time", return_value=1000), patch.object(b, "is_friend", return_value=False), patch("bridge.post") as send:
            b.forward(EVENT)
            send.assert_not_called()

    def test_send_unknown_not_retried(self):
        b = self.bridge()
        with patch.object(b, "is_friend", return_value=True), patch.object(b, "native_call", side_effect=TimeoutError) as send:
            with self.assertRaises(TimeoutError):
                b.send({"user_id": 87654321, "message": "hello", "auto_escape": True})
            send.assert_called_once()

    def test_send_friend_and_payload_limits(self):
        b = self.bridge()
        with patch.object(b, "is_friend", return_value=False), patch.object(b, "native_call") as send:
            self.assertEqual(b.send({"user_id": 87654321, "message": "hello", "auto_escape": True})["status"], "failed")
            send.assert_not_called()
        for payload in [{"user_id": 87654321, "message": "hello", "auto_escape": False},
                        {"user_id": 87654321, "message": [{"type": "image"}], "auto_escape": True},
                        {"user_id": 87654321, "message": "x" * 4001, "auto_escape": True},
                        {"user_id": 87654321, "message": "x", "auto_escape": True, "group_id": 1}]:
            with self.assertRaises(ValueError):
                b.send(payload)


if __name__ == "__main__":
    unittest.main()
