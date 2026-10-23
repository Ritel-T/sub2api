// Private /sub commands belong exclusively to the Sub2API bridge.
// Reserve before allowFrom, history, batching, reactions and model dispatch.
export function isSub2APIPrivateCommand(event) {
    if (event?.message_type !== "private") return false;
    const text = Array.isArray(event.message)
        ? event.message.filter(segment => segment?.type === "text")
            .map(segment => String(segment.data?.text ?? "")).join("")
        : String(event.raw_message ?? event.message ?? "");
    return /^\/sub(?:\s|$)/.test(text.trimStart());
}
