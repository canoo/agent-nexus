// Firefox event page: register synchronously; ignore the clicked tab entirely.
// No activity, origins, storage, user identifiers or production host are accessed.
const HOST = "com.codelogiic.nexus.firefox_probe";
let busy = false;

function validReply(reply) {
  return reply && typeof reply === "object" && !Array.isArray(reply)
    && Object.keys(reply).length === 4 && reply.schema_version === 1
    && reply.fixture_only === true && reply.browser_family === "firefox"
    && reply.ok === true;
}

browser.action.onClicked.addListener(() => {
  if (busy) return;
  busy = true;
  void browser.runtime.sendNativeMessage(HOST, {
    kind: "transport-probe", browser_family: "firefox", schema_version: 1,
  }).then(
    (reply) => browser.action.setBadgeText({ text: validReply(reply) ? "OK" : "ERR" }),
    () => browser.action.setBadgeText({ text: "ERR" }),
  ).catch(() => undefined).finally(() => { busy = false; });
});
