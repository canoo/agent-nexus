function quote(value) {
  return `'${String(value).replace(/'/g, "'\\''")}'`;
}

export function renderRuntimeLaunchers({ runtimeRoot, nodePath, desktopPath }) {
  const root = runtimeRoot.replace(/\/+$/, '');
  const chromeHost = `${root}/apps/companion-native-host/bin/nexus-companion-native-host-chrome.mjs`;
  const edgeHost = `${root}/apps/companion-native-host/bin/nexus-companion-native-host-edge.mjs`;
  const helper = `${root}/apps/companion-native-host/bin/nexus-companion-native-host-registration.mjs`;

  const chrome = `#!/bin/sh\nexec ${quote(nodePath)} ${quote(chromeHost)}\n`;
  const edge = `#!/bin/sh\nexec ${quote(nodePath)} ${quote(edgeHost)}\n`;
  const desktop = `#!/bin/sh
export NEXUS_REPO=${quote(runtimeRoot)}
export NEXUS_COMPANION_NODE=${quote(nodePath)}
export NEXUS_COMPANION_NATIVE_HOST_REGISTRATION_HELPER=${quote(helper)}
exec ${quote(desktopPath)} "$@"\n`;

  return { chrome, edge, desktop };
}
