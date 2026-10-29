#!/usr/bin/env node
import { runNativeMessagingHost } from "../lib/host.mjs";

runNativeMessagingHost({ browserFamily: "chrome" });
