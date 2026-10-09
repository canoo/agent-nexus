#!/usr/bin/env node
import { createNativeHostManifest, installNativeHost, uninstallNativeHost } from "../lib/registration.mjs";

function usage() {
  return "usage: nexus-companion-native-host-registration <print-manifest|install|uninstall> --browser <chrome|edge> [--extension-id <id>]... [--host-path </absolute/path>]";
}

function argumentsFrom(argv) {
  const [action, ...rest] = argv;
  const values = { action, extensionIds: [] };
  for (let index = 0; index < rest.length; index += 1) {
    const flag = rest[index];
    const value = rest[index + 1];
    if (flag === "--browser") values.browser = value;
    else if (flag === "--extension-id") values.extensionIds.push(value);
    else if (flag === "--host-path") values.hostPath = value;
    else throw new TypeError(`unsupported option: ${flag}`);
    index += 1;
  }
  return values;
}

try {
  const options = argumentsFrom(process.argv.slice(2));
  if (options.action === "print-manifest") {
    process.stdout.write(`${JSON.stringify(createNativeHostManifest(options), null, 2)}\n`);
  } else if (options.action === "install") {
    process.stdout.write(`${installNativeHost(options)}\n`);
  } else if (options.action === "uninstall") {
    process.stdout.write(`${uninstallNativeHost(options)}\n`);
  } else {
    throw new TypeError(usage());
  }
} catch (error) {
  // CLI errors contain only command flags/validation errors, never native input.
  process.stderr.write(`${error instanceof Error ? error.message : usage()}\n`);
  process.exitCode = 2;
}
