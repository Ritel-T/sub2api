"""Loopback-only HTTP contract tests; no QQ connection or real messages."""
import contextlib
import http.client
import json
import threading
import unittest
import urllib.error
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
from unittest.mock import patch
from bridge import Bridge, handler, post

@contextlib.contextmanager
def server(handler_type):
    srv = ThreadingHTTPServer(('127.0.0.1', 0), handler_type)
    srv.daemon_threads = True
    thread = threading.Thread(target=srv.serve_forever, daemon=True)
    thread.start()
    try:
        yield srv
    finally:
        srv.shutdown()
        srv.server_close()
        thread.join(2)

class HTTPTests(unittest.TestCase):
    def setUp(self):
        self.calls = []
        self.mode = 'sent'
        self.friend = True
        owner = self
        class Native(BaseHTTPRequestHandler):
            def log_message(self, *_): pass
            def do_POST(self):
                data = json.loads(self.rfile.read(int(self.headers['Content-Length'])))
                owner.calls.append((self.path, self.headers.get('Authorization'), data))
                if self.path == '/get_friend_list':
                    result = {'status':'ok','retcode':0,'data':[{'user_id':87654321}] if owner.friend else []}
                elif owner.mode == 'disconnect':
                    self.close_connection = True
                    return
                elif owner.mode == 'malformed':
                    self.send_response(200); self.end_headers(); self.wfile.write(b'not json'); return
                else:
                    result = {'status':'ok','retcode':0,'data':{'message_id':17}}
                raw = json.dumps(result).encode()
                self.send_response(200); self.send_header('Content-Length',str(len(raw))); self.end_headers(); self.wfile.write(raw)
        self.stack = contextlib.ExitStack()
        self.native = self.stack.enter_context(server(Native))
        self.bridge = Bridge.__new__(Bridge)
        self.bridge.native = 'http://127.0.0.1:'+str(self.native.server_port)
        self.bridge.native_token = 'native-test-only'
        self.bridge.relay_token = 'relay-test-only-'+'x'*32
        self.relay = self.stack.enter_context(server(handler(self.bridge)))
        self.addCleanup(self.stack.close)

    def request(self, payload=None, token=None, path='/send_private_msg', raw=None):
        conn = http.client.HTTPConnection('127.0.0.1', self.relay.server_port, timeout=3)
        try:
            body = raw if raw is not None else json.dumps(payload or {'user_id':87654321,'message':'hello','auto_escape':True})
            conn.request('POST',path,body,{'Authorization':'Bearer '+(token or self.bridge.relay_token),'Content-Type':'application/json'})
            result = conn.getresponse()
            return result.status, result.read()
        finally:
            conn.close()

    def test_auth_and_path_reject_before_native_io(self):
        self.assertEqual(self.request(token='wrong')[0],403)
        self.assertEqual(self.request(path='/get_friend_list')[0],404)
        self.assertEqual(self.request(path='/send_private_msg?extra=1')[0],404)
        self.assertEqual(self.calls,[])

    def test_payload_reject_before_native_io(self):
        for payload in [{'user_id':87654321,'message':'x','auto_escape':False}, {'user_id':87654321,'message':[{'type':'image'}],'auto_escape':True}, {'user_id':87654321,'message':'x','auto_escape':True,'group_id':5}]:
            self.assertEqual(self.request(payload)[0],400)
        self.assertEqual(self.request(raw='not json')[0],400)
        self.assertEqual(self.calls,[])

    def test_plain_cq_text_stays_auto_escaped_and_token_is_native_only(self):
        payload={'user_id':87654321,'message':'[CQ:image,file=https://example.invalid/test]','auto_escape':True}
        status, raw = self.request(payload)
        self.assertEqual(status,200)
        self.assertEqual(json.loads(raw)['data']['message_id'],17)
        self.assertEqual([x[0] for x in self.calls],['/get_friend_list','/send_private_msg'])
        self.assertEqual(self.calls[1][2],payload)
        self.assertTrue(all(x[1]=='Bearer native-test-only' for x in self.calls))

    def test_nonfriend_rejected_without_send(self):
        self.friend=False
        status, raw=self.request()
        self.assertEqual(status,200)
        self.assertEqual(json.loads(raw)['status'],'failed')
        self.assertEqual([x[0] for x in self.calls],['/get_friend_list'])

    def test_unknown_after_send_never_retried_or_reported_as_validation(self):
        for mode in ['disconnect','malformed']:
            with self.subTest(mode=mode):
                self.mode=mode
                self.calls.clear()
                self.assertEqual(self.request()[0],502)
                self.assertEqual([x[0] for x in self.calls],['/get_friend_list','/send_private_msg'])

    def test_native_bypasses_environment_proxy(self):
        with patch.dict('os.environ', {'http_proxy':'http://127.0.0.1:1','HTTP_PROXY':'http://127.0.0.1:1','no_proxy':'','NO_PROXY':''}), patch('urllib.request.proxy_bypass',return_value=False):
            self.assertEqual(self.request()[0],200)
        self.assertEqual(len(self.calls),2)

    def test_webhook_204_and_redirect_not_followed(self):
        calls=[]
        class Endpoint(BaseHTTPRequestHandler):
            def log_message(self,*_): pass
            def do_POST(self):
                calls.append(self.path)
                self.rfile.read(int(self.headers['Content-Length']))
                if self.path=='/redirect':
                    self.send_response(307); self.send_header('Location','/unexpected')
                else:
                    self.send_response(204)
                self.end_headers()
        with server(Endpoint) as local:
            url='http://127.0.0.1:'+str(local.server_port)
            self.assertIsNone(post(url+'/webhook',b'{}',{}))
            with self.assertRaises(urllib.error.HTTPError):
                post(url+'/redirect',b'{}',{'Authorization':'test'})
        self.assertEqual(calls,['/webhook','/redirect'])

if __name__=='__main__': unittest.main()