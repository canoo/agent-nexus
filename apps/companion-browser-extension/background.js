import {
  discardUnconsentedSpans,
  dispatchToNativeHost,
  isFocusedSelectedTab,
  sanitizeActiveSpans,
  transitionAllSpansInactive,
  transitionSelectedTab,
} from "./lib/activity.js";
import { createEventQueue } from "./lib/event-queue.js";
import { TOOL_DEFINITIONS, TOOL_IDS, originForUrl, sanitizeConsents } from "./lib/policy.js";

const ACTIVE_SPANS_KEY = "activeSpans";
const CONSENTS_KEY = "toolConsents";
const NATIVE_HOST_NAME = "com.codelogiic.nexus.companion";
const queue = createEventQueue();
const blockedTools = new Set();
const consentRevisions = Object.fromEntries(TOOL_IDS.map((id) => [id, 0]));
let deliveriesInFlight = 0;
const MAX_DELIVERIES_IN_FLIGHT = 4;

// Exposes completion only, never activity or consent state.
export function whenIdle() { return queue.idle(); }

function schedule(task) { void queue.enqueue(task).catch(() => undefined); }

function browserFamily() {
  return navigator.userAgent.includes("Edg/") ? "edge" : "chrome";
}

function platform() {
  const userAgent = navigator.userAgent.toLowerCase();
  if (userAgent.includes("linux")) return "linux";
  if (userAgent.includes("mac os")) return "macos";
  return undefined;
}

function effectiveConsents(raw) {
  const consents = { ...sanitizeConsents(raw) };
  for (const id of blockedTools) consents[id] = false;
  return consents;
}

async function readState() {
  const [session, local] = await Promise.all([
    chrome.storage.session.get(ACTIVE_SPANS_KEY),
    chrome.storage.local.get(CONSENTS_KEY),
  ]);
  return {
    activeSpans: sanitizeActiveSpans(session[ACTIVE_SPANS_KEY]),
    consents: effectiveConsents(local[CONSENTS_KEY]),
  };
}

async function saveSpans(spans) {
  // Revocation may have arrived while a browser/storage promise was pending.
  const next = sanitizeActiveSpans(spans);
  for (const [windowId, span] of Object.entries(next)) {
    if (blockedTools.has(span.tool_id)) delete next[windowId];
  }
  await chrome.storage.session.set({ [ACTIVE_SPANS_KEY]: next });
}

function deliver(event) {
  if (!event || blockedTools.has(event.tool_id) || deliveriesInFlight >= MAX_DELIVERIES_IN_FLIGHT) return;
  deliveriesInFlight += 1;
  // A stalled host must not hold the state queue or grow an unbounded backlog.
  void dispatchToNativeHost(event, (payload) => chrome.runtime.sendNativeMessage(NATIVE_HOST_NAME, payload))
    .finally(() => { deliveriesInFlight -= 1; });
}

async function applyTransition(windowId, origin, now) {
  const { activeSpans, consents } = await readState();
  const result = transitionSelectedTab({
    activeSpans, windowId, origin, consents, now,
    browserFamily: browserFamily(), platform: platform(),
  });
  await saveSpans(result.activeSpans);
  deliver(result.event);
}

async function observeSelectedTab(windowId, tabId, now) {
  const [tab, browserWindow] = await Promise.all([chrome.tabs.get(tabId), chrome.windows.get(windowId)]);
  await applyTransition(
    windowId,
    isFocusedSelectedTab({ tabActive: tab.active, windowFocused: browserWindow.focused }) ? originForUrl(tab.url) : null,
    now,
  );
}

async function endAllSpansForLostFocus(now) {
  const { activeSpans, consents } = await readState();
  const result = transitionAllSpansInactive({
    activeSpans, consents, now,
    browserFamily: browserFamily(), platform: platform(),
  });
  await saveSpans(result.activeSpans);
  for (const event of result.events) deliver(event);
}

async function reconcileRemovedPermissions(toolIds) {
  const [session, local] = await Promise.all([
    chrome.storage.session.get(ACTIVE_SPANS_KEY), chrome.storage.local.get(CONSENTS_KEY),
  ]);
  const nextConsents = { ...sanitizeConsents(local[CONSENTS_KEY]) };
  for (const id of toolIds) nextConsents[id] = false;
  await chrome.storage.local.set({ [CONSENTS_KEY]: nextConsents });
  await saveSpans(discardUnconsentedSpans(session[ACTIVE_SPANS_KEY], nextConsents));
}

async function reconcileConsentSnapshot(snapshot, revisions) {
  const session = await chrome.storage.session.get(ACTIVE_SPANS_KEY);
  // Preserve the false snapshot even if a later grant is already in storage.
  await saveSpans(discardUnconsentedSpans(session[ACTIVE_SPANS_KEY], snapshot));
  for (const tool of TOOL_DEFINITIONS) {
    if (!snapshot[tool.id] || !blockedTools.has(tool.id)) continue;
    const granted = await chrome.permissions.contains({ origins: [...tool.origins] });
    // An older grant must never undo a newer revocation received during await.
    if (granted && consentRevisions[tool.id] === revisions[tool.id]) blockedTools.delete(tool.id);
  }
}

chrome.tabs.onActivated.addListener(({ tabId, windowId }) => {
  const now = new Date().toISOString();
  schedule(() => observeSelectedTab(windowId, tabId, now));
});

chrome.tabs.onUpdated.addListener((tabId, changeInfo, tab) => {
  if (tab.active && changeInfo.status === "complete") {
    const windowId = tab.windowId;
    const now = new Date().toISOString();
    schedule(() => observeSelectedTab(windowId, tabId, now));
  }
});

chrome.windows.onFocusChanged.addListener((windowId) => {
  const now = new Date().toISOString();
  schedule(async () => {
    if (windowId === chrome.windows.WINDOW_ID_NONE) return endAllSpansForLostFocus(now);
    const tabs = await chrome.tabs.query({ active: true, windowId });
    if (tabs[0]) await observeSelectedTab(windowId, tabs[0].id, now);
  });
});

chrome.windows.onRemoved.addListener((windowId) => {
  const now = new Date().toISOString();
  schedule(() => applyTransition(windowId, null, now));
});

chrome.permissions.onRemoved.addListener((permissions) => {
  const removed = new Set(Array.isArray(permissions.origins) ? permissions.origins : []);
  const toolIds = TOOL_DEFINITIONS.filter((tool) => tool.origins.some((origin) => removed.has(origin))).map((tool) => tool.id);
  for (const id of toolIds) {
    blockedTools.add(id);
    consentRevisions[id] += 1;
  }
  if (toolIds.length) schedule(() => reconcileRemovedPermissions(toolIds));
});

chrome.storage.onChanged.addListener((changes, areaName) => {
  if (areaName !== "local" || !Object.hasOwn(changes, CONSENTS_KEY)) return;
  const snapshot = sanitizeConsents(changes[CONSENTS_KEY].newValue);
  const revisions = {};
  for (const id of TOOL_IDS) {
    consentRevisions[id] += 1;
    revisions[id] = consentRevisions[id];
    if (!snapshot[id]) blockedTools.add(id);
  }
  schedule(() => reconcileConsentSnapshot(snapshot, revisions));
});
