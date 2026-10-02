import { dispatchToNativeHost, sanitizeActiveSpans, transitionSelectedTab } from "./lib/activity.js";
import { originForUrl, sanitizeConsents } from "./lib/policy.js";

const ACTIVE_SPANS_KEY = "activeSpans";
const CONSENTS_KEY = "toolConsents";
const NATIVE_HOST_NAME = "com.codelogiic.nexus.companion";

function browserFamily() {
  return navigator.userAgent.includes("Edg/") ? "edge" : "chrome";
}

function platform() {
  const userAgent = navigator.userAgent.toLowerCase();
  if (userAgent.includes("linux")) return "linux";
  if (userAgent.includes("mac os")) return "macos";
  return undefined;
}

async function readState() {
  const [session, local] = await Promise.all([
    chrome.storage.session.get(ACTIVE_SPANS_KEY),
    chrome.storage.local.get(CONSENTS_KEY),
  ]);
  return {
    activeSpans: sanitizeActiveSpans(session[ACTIVE_SPANS_KEY]),
    consents: sanitizeConsents(local[CONSENTS_KEY]),
  };
}

async function applyTransition(windowId, origin) {
  const { activeSpans, consents } = await readState();
  const result = transitionSelectedTab({
    activeSpans,
    windowId,
    origin,
    consents,
    now: new Date().toISOString(),
    browserFamily: browserFamily(),
    platform: platform(),
  });
  await chrome.storage.session.set({ [ACTIVE_SPANS_KEY]: result.activeSpans });
  // Deliberately ignore the result: unavailable hosts must fail closed.
  await dispatchToNativeHost(result.event, (event) => chrome.runtime.sendNativeMessage(NATIVE_HOST_NAME, event));
}

async function observeSelectedTab(windowId, tabId) {
  try {
    const tab = await chrome.tabs.get(tabId);
    await applyTransition(windowId, tab.active ? originForUrl(tab.url) : null);
  } catch {
    // Browser APIs can race a closed tab. There is no event and no payload log.
  }
}

chrome.tabs.onActivated.addListener(({ tabId, windowId }) => { void observeSelectedTab(windowId, tabId); });

chrome.tabs.onUpdated.addListener((tabId, changeInfo, tab) => {
  if (tab.active && changeInfo.status === "complete") void observeSelectedTab(tab.windowId, tabId);
});

chrome.windows.onFocusChanged.addListener((windowId) => {
  if (windowId === chrome.windows.WINDOW_ID_NONE) return;
  void chrome.tabs.query({ active: true, windowId }).then((tabs) => {
    if (tabs[0]) return observeSelectedTab(windowId, tabs[0].id);
    return undefined;
  }).catch(() => undefined);
});

chrome.windows.onRemoved.addListener((windowId) => { void applyTransition(windowId, null).catch(() => undefined); });
