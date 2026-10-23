#!/usr/bin/env python3
"""QQ ingress and narrowly scoped send relay. Never retries a message send."""
import asyncio
import hashlib
import hmac
import json
import logging
import os
import re
import time
import urllib.error
import urllib.parse
import urllib.request
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer

LOG = logging.getLogger("sub2api-qq")
PREFIX = re.compile(r"^/sub(?:\s|$)")
QQ = re.compile(r"^[1-9][0-9]{4,19}$")


class SendValidationError(ValueError):
    pass


class NoRedirect(urllib.request.HTTPRedirectHandler):
    def redirect_request(self, *args, **kwargs):
        return None


def post(url, body, headers):
    req = urllib.request.Request(url, data=body, headers={"Content-Type": "application/json", **headers})
    # Native credentials must never leave loopback through environment proxies.
    handlers = [NoRedirect()]
    if urllib.parse.urlsplit(url).hostname in ('127.0.0.1', '::1', 'localhost'):
        handlers.append(urllib.request.ProxyHandler({}))
    try:
        opened = urllib.request.build_opener(*handlers).open(req, timeout=12)
    except urllib.error.HTTPError as error:
        error.close()
        raise
    with opened as response:
        raw = response.read(65537)
        if len(raw) > 65536:
            raise ValueError("response too large")
        if response.status == 204 and not raw:
            return None
        return json.loads(raw)


def admitted(event, bot, now=None):
    now = time.time() if now is None else now
    if not isinstance(event, dict):
        return False
    if (event.get("post_type"), event.get("message_type"), event.get("sub_type")) != ("message", "private", "friend"):
        return False
    if str(event.get("self_id")) != bot or not QQ.fullmatch(str(event.get("user_id", ""))):
        return False
    stamp = event.get("time")
    if type(stamp) is not int or abs(now - stamp) > 300:
        return False
    segments = event.get("message")
    if not isinstance(segments, list) or len(segments) != 1 or not isinstance(segments[0], dict):
        return False
    seg = segments[0]
    data = seg.get("data")
    text = data.get("text") if isinstance(data, dict) else None
    return seg.get("type") == "text" and isinstance(text, str) and len(text) <= 256 and bool(PREFIX.match(text.strip()))


class Bridge:
    def __init__(self):
        self.bot = os.environ["QQ_BOT_ID"]
        self.native_token = os.environ["QQ_NATIVE_TOKEN"]
        self.relay_token = os.environ["QQ_RELAY_TOKEN"]
        self.secret = os.environ["QQ_WEBHOOK_SECRET"].encode()
        self.webhook = os.environ["QQ_WEBHOOK_URL"]
        self.native = "http://127.0.0.1:3000"
        self.ws = "ws://127.0.0.1:3001"
        parsed = urllib.parse.urlsplit(self.webhook)
        if parsed.scheme != "https" or not parsed.hostname or parsed.username is not None or parsed.query or parsed.fragment or parsed.path != "/api/v1/qq/webhook":
            raise ValueError("webhook must be an HTTPS /api/v1/qq/webhook endpoint")
        if not QQ.fullmatch(self.bot) or len(self.secret) < 32 or len(self.relay_token) < 32 or not self.native_token:
            raise ValueError("invalid bridge configuration")
        if self.relay_token == self.native_token:
            raise ValueError("relay and native credentials must differ")

    def native_call(self, action, payload):
        return post(self.native + "/" + action, json.dumps(payload).encode(), {"Authorization": "Bearer " + self.native_token})

    def is_friend(self, user):
        result = self.native_call("get_friend_list", {})
        return (isinstance(result, dict) and result.get('status') == 'ok'
                and result.get('retcode') == 0 and isinstance(result.get('data'), list)
                and any(str(x.get('user_id')) == str(user) for x in result['data'] if isinstance(x, dict)))

    def forward(self, event):
        if not admitted(event, self.bot) or not self.is_friend(event["user_id"]):
            return
        # Preserve only contract fields; never forward nicknames or chat history.
        clean = {k: event[k] for k in ("post_type", "message_type", "sub_type", "self_id", "user_id", "message_id", "time", "message")}
        body = json.dumps(clean, ensure_ascii=False, separators=(",", ":")).encode()
        signature = hmac.new(self.secret, body, hashlib.sha256).hexdigest()
        post(self.webhook, body, {"X-QQ-Signature": "sha256=" + signature})

    def send(self, data):
        if set(data) != {"user_id", "message", "auto_escape"} or data.get("auto_escape") is not True:
            raise SendValidationError("invalid send fields")
        if not QQ.fullmatch(str(data.get("user_id", ""))) or not isinstance(data.get("message"), str) or not 0 < len(data["message"]) <= 4000:
            raise SendValidationError("invalid message")
        if not self.is_friend(data["user_id"]):
            return {"status": "failed", "retcode": 1404, "data": None}
        # Exactly one invocation; network/parse exceptions must remain unknown.
        return self.native_call("send_private_msg", data)


def handler(bridge):
    class Handler(BaseHTTPRequestHandler):
        def log_message(self, *_):
            pass

        def do_POST(self):
            if self.path != "/send_private_msg":
                self.send_error(404)
                return
            auth = self.headers.get("Authorization", "")
            if not hmac.compare_digest(auth.encode(), ("Bearer " + bridge.relay_token).encode()):
                self.send_error(403)
                return
            try:
                count = int(self.headers.get("Content-Length", "0"))
                if not 0 < count <= 32768 or self.headers.get("Transfer-Encoding"):
                    raise ValueError("invalid length")
                self.connection.settimeout(15)
                value = json.loads(self.rfile.read(count))
                if not isinstance(value, dict):
                    raise ValueError("invalid payload")
            except (ValueError, TimeoutError):
                self.send_error(400)
                return
            try:
                result = bridge.send(value)
                body = json.dumps(result).encode()
                self.send_response(200)
                self.send_header("Content-Type", "application/json")
                self.send_header("Content-Length", str(len(body)))
                self.end_headers()
                self.wfile.write(body)
            except SendValidationError:
                self.send_error(400)
            except Exception:
                LOG.warning("relay send outcome unknown; not retried")
                self.send_error(502)
    return Handler


async def receive(bridge):
    from websockets.asyncio.client import connect
    while True:
        try:
            async with connect(bridge.ws, additional_headers={"Authorization": "Bearer " + bridge.native_token}, max_size=65536, max_queue=16, proxy=None) as ws:
                async for raw in ws:
                    try:
                        event = json.loads(raw)
                        if admitted(event, bridge.bot):
                            await asyncio.to_thread(bridge.forward, event)
                    except Exception:
                        LOG.warning("incoming command failed or outcome unknown; not replayed")
        except Exception:
            LOG.warning("OneBot disconnected; reconnecting in 5 seconds")
            await asyncio.sleep(5)


async def main():
    bridge = Bridge()
    server = ThreadingHTTPServer(("127.0.0.1", 3102), handler(bridge))
    server.daemon_threads = True
    try:
        await asyncio.gather(asyncio.to_thread(server.serve_forever), receive(bridge))
    finally:
        server.shutdown()


if __name__ == "__main__":
    logging.basicConfig(level=logging.INFO)
    asyncio.run(main())
