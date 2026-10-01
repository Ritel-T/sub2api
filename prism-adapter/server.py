"""Loopback-only Prism browser adapter for OpenAI OAuth accounts.

Prism's web UI owns session, sandbox, and start/status requests. This adapter
observes their terminal result and never fabricates token deltas or usage.
"""

import hmac
import hashlib
import json
import os
import re
import threading
import time
import sys
import traceback
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
from pathlib import Path
from urllib.parse import parse_qs, urlparse

from playwright.sync_api import sync_playwright


BASE = "https://prism.openai.com"
START = "/api/llm/response_with_tools_start"
STATUS = "/api/llm/response_with_tools_status"
MAX_REQUEST_BYTES = 1 << 20
MAX_PROMPT_CHARS = 32000
MODEL = "gpt-5.6-sol"
PROJECT_ID = re.compile(r"^[0-9a-f]{8}-[0-9a-f-]{27,}$")
ACCOUNT_ID = re.compile(r"^[1-9][0-9]{0,18}$")
USER_AGENT = "Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36 Chrome/153 Safari/537.36"


class AdapterError(Exception):
    def __init__(self, status, code, message):
        super().__init__(message)
        self.status = status
        self.code = code


def parse_prompt(payload):
    if not isinstance(payload, dict) or payload.get("model") != MODEL:
        raise AdapterError(422, "unsupported_model", "This Prism account currently supports gpt-5.6-sol only")
    if payload.get("tools") or payload.get("additional_tools") or payload.get("previous_response_id") or payload.get("conversation"):
        raise AdapterError(422, "unsupported_request", "Prism adapter does not yet support tools or server-side conversation state")
    if any(payload.get(key) is not None for key in ("max_output_tokens", "temperature", "top_p")) or payload.get("background") or payload.get("store"):
        raise AdapterError(422, "unsupported_request", "Generation limits, sampling, background and storage options are not supported")
    if payload.get("tool_choice", "none") not in ("none", "auto") or payload.get("include") or payload.get("service_tier"):
        raise AdapterError(422, "unsupported_request", "Requested response options are not supported")
    text_options = payload.get("text") or {}
    if not isinstance(text_options, dict) or text_options.get("format", {"type": "text"}) != {"type": "text"}:
        raise AdapterError(422, "unsupported_request", "Only plain text output is supported")
    reasoning = payload.get("reasoning") or {}
    if not isinstance(reasoning, dict) or reasoning.get("effort", "medium") != "medium" or reasoning.get("summary") not in (None, "none"):
        raise AdapterError(422, "unsupported_reasoning", "Prism browser currently provides medium reasoning only")
    if not isinstance(payload.get("stream", False), bool):
        raise AdapterError(400, "invalid_request", "stream must be a boolean")
    items = payload.get("input")
    if isinstance(items, str):
        items = [{"role": "user", "content": items}]
    if not isinstance(items, list) or not items:
        raise AdapterError(400, "invalid_request", "input must contain text")
    parts = []
    instructions = payload.get("instructions", "")
    if instructions:
        if not isinstance(instructions, str):
            raise AdapterError(400, "invalid_request", "instructions must be text")
        parts.append("[instructions]\n" + instructions)
    for item in items:
        if not isinstance(item, dict) or item.get("type", "message") != "message":
            raise AdapterError(422, "unsupported_input", "Prism adapter accepts text messages only")
        role = item.get("role", "user")
        if role not in ("user", "assistant", "system", "developer"):
            raise AdapterError(400, "invalid_request", "invalid message role")
        content = item.get("content")
        if isinstance(content, str):
            text = content
        elif isinstance(content, list) and content and all(isinstance(x, dict) and x.get("type") in ("input_text", "output_text", "text") and isinstance(x.get("text"), str) for x in content):
            text = "\n".join(x["text"] for x in content)
        else:
            raise AdapterError(422, "unsupported_input", "Prism adapter accepts text messages only")
        if not text.strip():
            raise AdapterError(400, "invalid_request", "message content must not be empty")
        parts.append("[" + role + "]\n" + text)
    prompt = "\n\n".join(parts)
    if not prompt.strip() or len(prompt) > MAX_PROMPT_CHARS:
        raise AdapterError(400, "invalid_request", "text input is empty or too long")
    return prompt, payload.get("stream", False)


def terminal_text(data):
    if not isinstance(data, dict):
        return None
    response = data.get("response") or {}
    if data.get("status") not in ("completed", "failed", "error"):
        return None
    if data.get("status") in ("failed", "error"):
        return AdapterError(502, "prism_failed", "Prism turn failed")
    if not isinstance(response, dict) or response.get("status") not in ("success", "failed", "error"):
        return None
    if response.get("status") in ("failed", "error"):
        return AdapterError(502, "prism_failed", "Prism turn failed")
    output = (response.get("payload") or {}).get("output") or []
    texts = [part.get("text", "") for item in output if isinstance(item, dict) and item.get("type") == "message"
             for part in item.get("content", []) if isinstance(part, dict) and isinstance(part.get("text"), str)]
    if not texts:
        return AdapterError(502, "unsupported_output", "Prism returned no text message")
    return "".join(texts)


class State:
    def __init__(self, directory):
        self.directory = Path(directory)
        self.directory.mkdir(mode=0o700, parents=True, exist_ok=True)
        self.pending = self.directory / "pending"
        self.pending.mkdir(mode=0o700, exist_ok=True)
        self.projects = self.directory / "projects.json"
        self.receipts = self.directory / "receipts"
        self.receipts.mkdir(mode=0o700, exist_ok=True)

    @staticmethod
    def sync_directory(directory):
        fd = os.open(directory, os.O_RDONLY | os.O_DIRECTORY)
        try:
            os.fsync(fd)
        finally:
            os.close(fd)

    @staticmethod
    def atomic_write(path, data):
        tmp = path.with_name(path.name + ".tmp")
        fd = os.open(tmp, os.O_CREAT | os.O_TRUNC | os.O_WRONLY, 0o600)
        with os.fdopen(fd, "w") as file:
            json.dump(data, file)
            file.flush()
            os.fsync(file.fileno())
        os.replace(tmp, path)
        State.sync_directory(path.parent)

    def ensure_idle(self, account_id):
        if (self.pending / account_id).exists():
            raise AdapterError(409, "pending_turn", "Previous Prism turn outcome is unknown; inspect it before a new request")

    def project(self, account_id):
        try:
            project = json.loads(self.projects.read_text()).get(account_id)
        except FileNotFoundError:
            return None
        except (ValueError, OSError):
            raise AdapterError(503, "invalid_state", "Prism project state is unreadable") from None
        return project if isinstance(project, str) and PROJECT_ID.fullmatch(project) else None

    def save_project(self, account_id, project_id):
        if not PROJECT_ID.fullmatch(project_id):
            raise AdapterError(502, "invalid_project", "Prism returned an invalid project identifier")
        try:
            projects = json.loads(self.projects.read_text())
        except FileNotFoundError:
            projects = {}
        if not isinstance(projects, dict):
            raise AdapterError(503, "invalid_state", "Prism project state is invalid")
        projects[account_id] = project_id
        self.atomic_write(self.projects, projects)

    def begin(self, account_id, project_id=None):
        try:
            fd = os.open(self.pending / account_id, os.O_CREAT | os.O_EXCL | os.O_WRONLY, 0o600)
        except FileExistsError:
            raise AdapterError(409, "pending_turn", "Previous Prism turn outcome is unknown; inspect it before a new request") from None
        with os.fdopen(fd, "w") as file:
            json.dump({"stage": "submitting", "project_id": project_id, "at": int(time.time())}, file)
            file.flush()
            os.fsync(file.fileno())
        self.sync_directory(self.pending)

    def finish(self, account_id):
        (self.pending / account_id).unlink()
        self.sync_directory(self.pending)

    def update(self, account_id, data):
        path = self.pending / account_id
        previous = json.loads(path.read_text())
        previous.update(data)
        self.atomic_write(path, previous)

    def receipt(self, account_id, request_id, start_count, status_count, result):
        receipt_id = hashlib.sha256(request_id.encode()).hexdigest()
        data = {"account_id": account_id, "request_id": request_id, "model": MODEL,
                "start_count": start_count, "status_count": status_count, "completed_at": int(time.time()),
                "status": "failed" if isinstance(result, AdapterError) else "completed", "usage_source": "unavailable"}
        if isinstance(result, str):
            data["answer_sha256"] = hashlib.sha256(result.encode()).hexdigest()
            data["answer_chars"] = len(result)
        self.atomic_write(self.receipts / (receipt_id + ".json"), data)


class StartGate:
    """Authorize one exact start; browser retries are rejected before sending."""
    def __init__(self):
        self.armed = False
        self.sent = False
        self.error = None

    def accept(self, body):
        metadata = body.get("metadata") if isinstance(body, dict) else None
        if (not self.armed or self.sent or not isinstance(metadata, dict)
                or metadata.get("model") != MODEL or metadata.get("reasoning_effort") != "medium"):
            self.error = "Prism attempted an unarmed, repeated or mismatched model start"
            return False
        self.sent = True
        return True


class BrowserTurn:
    def __init__(self, state, chrome):
        self.state = state
        self.chrome = chrome

    def run(self, account_id, token, prompt):
        self.state.ensure_idle(account_id)
        with sync_playwright() as playwright:
            browser = playwright.chromium.launch(executable_path=self.chrome, headless=True, chromium_sandbox=True)
            began = False
            try:
                context = browser.new_context(user_agent=USER_AGENT, service_workers="block")
                context.add_cookies([{"name": "prism_oai_access_token", "value": token,
                                      "domain": "prism.openai.com", "path": "/", "secure": True}])
                page = context.new_page()
                page.set_default_timeout(60000)
                gate = StartGate()
                start_request_id = []
                def gate_start(route):
                    try:
                        request = route.request
                        if request.url == BASE + STATUS:
                            body = request.post_data_json
                            if (gate.sent and start_request_id and isinstance(body, dict)
                                    and body.get("request_id") == start_request_id[-1]):
                                route.continue_()
                                return
                            route.abort()
                            return
                        accepted = request.url == BASE + START and gate.accept(request.post_data_json)
                    except Exception:
                        accepted = False
                        gate.error = "Prism start request could not be validated"
                    if accepted:
                        route.continue_()
                    else:
                        route.abort()
                page.route("**/api/llm/response_with_tools_*", gate_start)
                # A new chat does not clear files in a Prism project. Every
                # stateless request gets a blank project, including admin tests.
                page.goto(BASE, wait_until="domcontentloaded", timeout=60000)
                page.get_by_role("button", name="New", exact=True).click(timeout=60000)
                page.get_by_role("menuitem", name="Blank project").click(timeout=60000)
                page.wait_for_function("new URL(location.href).searchParams.has('u')", timeout=60000)
                project = parse_qs(urlparse(page.url).query).get("u", [""])[0]
                self.state.save_project(account_id, project)
                # Enter the persisted project route only after creation; the
                # transient create-project view can expose an editor before
                # its chat/model state has initialized.
                page.goto(BASE + "/?u=" + project + "&pg=1", wait_until="domcontentloaded", timeout=60000)
                textarea = page.locator('textarea[placeholder="Ask anything"]')
                textarea.wait_for(state="visible", timeout=90000)
                model_button = page.get_by_role("button", name=re.compile(r"5\.6 Sol"))
                try:
                    model_button.wait_for(state="visible", timeout=90000)
                except Exception:
                    raise AdapterError(422, "unsupported_model", "gpt-5.6-sol is unavailable in this Prism account")

                starts = []
                terminal = []
                polls = []

                def on_request(request):
                    if request.url == BASE + START:
                        try:
                            body = request.post_data_json
                            starts.append((body.get("metadata") or {}).get("model"))
                        except (ValueError, AttributeError):
                            starts.append(None)
                    elif request.url == BASE + STATUS:
                        polls.append(True)

                def on_response(response):
                    if response.url not in (BASE + START, BASE + STATUS):
                        return
                    try:
                        data = response.json()
                        if not isinstance(data, dict):
                            return
                        if start_request_id and data.get("request_id") not in (None, "", start_request_id[-1]):
                            gate.error = "Prism returned a foreign request identifier"
                            return
                        if response.url == BASE + START and isinstance(data.get("request_id"), str) and data["request_id"]:
                            start_request_id.append(data["request_id"])
                        if start_request_id:
                            update = {"stage": "polling", "request_id": start_request_id[-1]}
                            if isinstance(data.get("turn_state"), str) and data["turn_state"]:
                                update["turn_state"] = data["turn_state"]
                            self.state.update(account_id, update)
                        result = terminal_text(data)
                        if result is not None:
                            terminal.append((response.status, data.get("request_id", ""), result))
                    except (ValueError, TypeError):
                        pass

                page.on("request", on_request)
                page.on("response", on_response)
                textarea.fill(prompt)
                self.state.begin(account_id, project)
                began = True
                gate.armed = True
                textarea.press("Enter")
                deadline = time.monotonic() + 240
                while time.monotonic() < deadline and not terminal and not gate.error:
                    page.wait_for_timeout(500)
                if not gate.sent:
                    raise AdapterError(502, "start_not_sent", "Prism did not submit the turn; no request was sent upstream")
                if gate.error or len(starts) != 1 or starts[0] != MODEL:
                    raise AdapterError(502, "unexpected_start", "Prism did not start exactly one turn with the requested model")
                if not terminal:
                    raise AdapterError(504, "unknown_outcome", "Prism turn has no terminal result; pending state retained")
                status, request_id, result = terminal[-1]
                request_id = request_id or (start_request_id[-1] if start_request_id else "")
                if status != 200:
                    raise AdapterError(502, "prism_failed", "Prism returned a failed turn")
                if isinstance(result, AdapterError):
                    self.state.receipt(account_id, request_id, len(starts), len(polls), result)
                    self.state.finish(account_id)
                    raise result
                if not request_id:
                    raise AdapterError(502, "missing_request_id", "Prism turn lacks a request identifier; pending state retained")
                self.state.receipt(account_id, request_id, len(starts), len(polls), result)
                self.state.finish(account_id)
                return request_id, result
            finally:
                browser.close()
                # The gate is the only way a start leaves the browser. If it never
                # released one, nothing reached Prism and the outcome is known, so
                # the account must not stay locked behind a 409. Checked after
                # close so a start released while closing still keeps the lock.
                if began and not gate.sent:
                    self.state.finish(account_id)


def response_payload(request_id, text):
    safe_id = re.sub(r"[^a-zA-Z0-9_-]", "_", request_id)[:100]
    if not safe_id:
        raise AdapterError(502, "missing_request_id", "Prism did not return a request identifier")
    return {
        "id": "resp_prism_" + safe_id,
        "object": "response",
        "created_at": int(time.time()),
        "model": MODEL,
        "status": "completed",
        "usage": None,
        "output": [{"id": "msg_prism_" + safe_id, "type": "message", "role": "assistant",
                    "status": "completed", "content": [{"type": "output_text", "text": text, "annotations": []}]}],
    }


class Handler(BaseHTTPRequestHandler):
    protocol_version = "HTTP/1.1"
    state = None
    browser_turn = None
    api_key = None
    lock = threading.Lock()

    def setup(self):
        super().setup()
        self.connection.settimeout(30)

    def log_message(self, *_args):
        pass

    def send_json(self, status, payload):
        body = json.dumps(payload, ensure_ascii=False, separators=(",", ":")).encode()
        self.send_response(status)
        self.send_header("Content-Type", "application/json")
        self.send_header("Content-Length", str(len(body)))
        self.send_header("Connection", "close")
        self.close_connection = True
        self.end_headers()
        self.wfile.write(body)

    def do_GET(self):
        if self.path == "/health":
            self.send_json(200, {"status": "ok"})
        else:
            self.send_json(404, {"error": {"type": "not_found"}})

    def do_POST(self):
        if self.path != "/v1/responses":
            self.send_json(404, {"error": {"type": "not_found"}})
            return
        bearer = self.headers.get("Authorization", "")
        if not hmac.compare_digest(bearer, "Bearer " + self.api_key):
            self.send_json(401, {"error": {"type": "unauthorized"}})
            return
        account_id = self.headers.get("X-Prism-Account-ID", "")
        token = self.headers.get("X-Prism-OAuth-Token", "")
        if not ACCOUNT_ID.fullmatch(account_id) or not token or "\n" in token or "\r" in token:
            self.send_json(400, {"error": {"type": "invalid_request", "message": "account identity is required"}})
            return
        try:
            if self.headers.get("Transfer-Encoding") or len(self.headers.get_all("Content-Length", [])) != 1:
                raise AdapterError(400, "invalid_request", "One Content-Length header is required")
            length = int(self.headers.get("Content-Length", "0"))
            if length < 1 or length > MAX_REQUEST_BYTES:
                raise AdapterError(413, "request_too_large", "request body is empty or too large")
            payload = json.loads(self.rfile.read(length))
            prompt, stream = parse_prompt(payload)
            if not self.lock.acquire(blocking=False):
                raise AdapterError(429, "prism_busy", "Prism browser is busy; request was not submitted")
            try:
                request_id, answer = self.browser_turn.run(account_id, token, prompt)
            finally:
                self.lock.release()
            response = response_payload(request_id, answer)
            if stream:
                created = dict(response, status="in_progress", output=[])
                events = [
                    ("response.created", {"type": "response.created", "sequence_number": 0, "response": created}),
                    ("response.completed", {"type": "response.completed", "sequence_number": 1, "response": response}),
                ]
                body = "".join("event: " + name + "\ndata: " + json.dumps(data, ensure_ascii=False, separators=(",", ":")) + "\n\n" for name, data in events).encode()
                self.send_response(200)
                self.send_header("Content-Type", "text/event-stream")
                self.send_header("Content-Length", str(len(body)))
                self.send_header("X-Request-Id", response["id"])
                self.send_header("Connection", "close")
                self.close_connection = True
                self.end_headers()
                self.wfile.write(body)
            else:
                self.send_json(200, response)
        except (BrokenPipeError, ConnectionResetError):
            self.close_connection = True
        except AdapterError as error:
            self.send_json(error.status, {"error": {"type": error.code, "message": str(error)}})
        except (ValueError, TypeError):
            self.send_json(400, {"error": {"type": "invalid_request", "message": "invalid JSON request"}})
        except Exception as error:
            # Never log exception messages: Playwright embeds headers and page
            # data in some failures. Frames and class identify the local phase.
            frames = traceback.extract_tb(error.__traceback__)
            own_frames = [frame.lineno for frame in frames if Path(frame.filename).name == "server.py"]
            print(json.dumps({"event": "prism_adapter_error", "class": type(error).__name__, "lines": own_frames}), file=sys.stderr, flush=True)
            self.send_json(502, {"error": {"type": "prism_unavailable", "message": "Prism browser request failed; inspect pending state before retrying"}})


def main():
    if os.geteuid() == 0:
        raise SystemExit("Prism adapter must run as a non-root user")
    key = os.environ.get("PRISM_ADAPTER_API_KEY", "")
    chrome = os.environ.get("PRISM_ADAPTER_CHROME", "")
    if len(key) < 32 or not Path(chrome).is_file() or not os.environ.get("CHROME_DEVEL_SANDBOX"):
        raise SystemExit("adapter key, Chromium binary, and Chromium sandbox are required")
    Handler.api_key = key
    Handler.state = State(os.environ.get("PRISM_ADAPTER_STATE_DIR", "/var/lib/sub2api-prism"))
    Handler.browser_turn = BrowserTurn(Handler.state, chrome)
    server = ThreadingHTTPServer(("127.0.0.1", 8319), Handler)
    server.daemon_threads = True
    server.serve_forever()


if __name__ == "__main__":
    main()
