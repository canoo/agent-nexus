import { existsSync } from "node:fs";

/** Native hosts run outside Flatpak; no sandbox escape or portal bridge exists. */
export function nativeHostRuntimeSupported({
  platform = process.platform,
  flatpakId = process.env.FLATPAK_ID,
  flatpakInfoExists = existsSync("/.flatpak-info"),
} = {}) {
  return (platform === "linux" || platform === "darwin")
    && (flatpakId === undefined || flatpakId === "")
    && flatpakInfoExists === false;
}
