import {
  discardUnconsentedSpans,
  dispatchToNativeHost,
  isFocusedSelectedTab,
  sanitizeActiveSpans,
  transitionAllSpansInactive,
  transitionSelectedTab,
} from "./lib/activity.js";
import { consentsEqual, reconcileConsentsAfterPermissionRemoval } from "./lib/consent.js";
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
    const [tab, browserWindow] = await Promise.all([chrome.tabs.get(tabId), chrome.windows.get(windowId)]);
    // An activation may arrive for a background window. Never start a span
    // until the browser confirms that exact window still has application focus.
    await applyTransition(
      windowId,
      isFocusedSelectedTab({ tabActive: tab.active, windowFocused: browserWindow.focused }) ? originForUrl(tab.url) : null,
    );
  } catch {
    // Browser APIs can race a closed tab. There is no event and no payload log.
  }
}

async function endAllSpansForLostFocus() {
  const { activeSpans, consents } = await readState();
  const result = transitionAllSpansInactive({
    activeSpans,
    consents,
    now: new Date().toISOString(),
    browserFamily: browserFamily(),
    platform: platform(),
  });
  await chrome.storage.session.set({ [ACTIVE_SPANS_KEY]: result.activeSpans });
  await Promise.all(result.events.map((event) => (
    dispatchToNativeHost(event, (payload) => chrome.runtime.sendNativeMessage(NATIVE_HOST_NAME, payload))
  )));
}

async function reconcileRemovedPermissions(removedOrigins) {
  const { activeSpans, consents } = await readState();
  const nextConsents = reconcileConsentsAfterPermissionRemoval(consents, removedOrigins);
  if (consentsEqual(consents, nextConsents)) return;
  await Promise.all([
    chrome.storage.local.set({ [CONSENTS_KEY]: nextConsents }),
    chrome.storage.session.set({ [ACTIVE_SPANS_KEY]: discardUnconsentedSpans(activeSpans, nextConsents) }),
  ]);
}

chrome.tabs.onActivated.addListener(({ tabId, windowId }) => { void observeSelectedTab(windowId, tabId); });

chrome.tabs.onUpdated.addListener((tabId, changeInfo, tab) => {
  if (tab.active && changeInfo.status === "complete") void observeSelectedTab(tab.windowId, tabId);
});

chrome.windows.onFocusChanged.addListener((windowId) => {
  if (windowId === chrome.windows.WINDOW_ID_NONE) {
    void endAllSpansForLostFocus().catch(() => undefined);
    return;
  }
  void chrome.tabs.query({ active: true, windowId }).then((tabs) => {
    if (tabs[0]) return observeSelectedTab(windowId, tabs[0].id);
    return undefined;
  }).catch(() => undefined);
});

chrome.windows.onRemoved.addListener((windowId) => { void applyTransition(windowId, null).catch(() => undefined); });

chrome.permissions.onRemoved.addListener((permissions) => {
  void reconcileRemovedPermissions(permissions.origins).catch(() => undefined);
});
