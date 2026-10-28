// Transport fixture only: no database, filesystem writes, or activity ingestion.
import { NativeMessageDecoder, encodeNativeMessage } from "./native-messaging.mjs";

const ADDON_ID = "nexus-firefox-probe@codelogiic.local";
const decoder = new NativeMessageDecoder({ maxMessageBytes: 1024 });
let finished = false;
const deadline = setTimeout(() => finish(false), 1000);
function finish(ok) {
  if (finished) return;
  finished = true;
  clearTimeout(deadline);
  process.stdin.destroy();
  process.stdout.write(encodeNativeMessage({
    schema_version: 1, fixture_only: true, browser_family: "firefox", ok,
  }));
}
function validProbe(value) {
  return !!value && typeof value === "object" && !Array.isArray(value)
    && Object.keys(value).length === 3 && value.kind === "transport-probe"
    && value.browser_family === "firefox" && value.schema_version === 1;
}
// Firefox supplies the host-manifest path followed by the initiating Gecko ID.
if (!process.argv[2]?.startsWith("/") || process.argv[3] !== ADDON_ID) finish(false);
else {
  process.stdin.on("data", (chunk) => {
    const frames = decoder.push(chunk);
    if (frames.length) finish(frames[0].ok && validProbe(frames[0].message));
  });
  process.stdin.on("end", () => finish(false));
  process.stdin.on("error", () => finish(false));
}
