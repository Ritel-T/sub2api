from pathlib import Path
import hashlib,difflib
root=Path(__file__).resolve().parent
original=(root/'openclaw-gateway.original.js').read_bytes()
assert hashlib.sha256(original).hexdigest()=='0b8db1326e469f867c1c238b9bfb0fb7928dd3e4e600768e70c366a00c95ef16'
s=original.decode()
helper='''// Private /sub commands belong exclusively to the Sub2API bridge.
// Reserve before allowFrom, history, batching, reactions and model dispatch.
export function isSub2APIPrivateCommand(event) {
    if (event?.message_type !== "private") return false;
    const text = Array.isArray(event.message)
        ? event.message.filter(segment => segment?.type === "text")
            .map(segment => String(segment.data?.text ?? "")).join("")
        : String(event.raw_message ?? event.message ?? "");
    return /^\\/sub(?:\\s|$)/.test(text.trimStart());
}
'''
anchor='// Reconnect configuration\n'
assert s.count(anchor)==1
s=s.replace(anchor,helper+anchor)
anchor2='''                if (!event || isAborted || abortSignal.aborted) return;
                const isGroup = event.message_type === "group";'''
assert s.count(anchor2)==1
s=s.replace(anchor2,'''                if (!event || isAborted || abortSignal.aborted) return;
                if (isSub2APIPrivateCommand(event)) return;
                const isGroup = event.message_type === "group";''')
(root/'openclaw-gateway.candidate.js').write_bytes(s.encode())
patch=''.join(difflib.unified_diff(original.decode().splitlines(True),s.splitlines(True),fromfile='a/dist/src/gateway.js',tofile='b/dist/src/gateway.js'))
(root/'openclaw-sub-command-reservation.patch').write_text(patch)
(root/'openclaw-sub-command-reservation.mjs').write_text(helper.replace('export function','export function'))
print('candidate_sha256='+hashlib.sha256(s.encode()).hexdigest())