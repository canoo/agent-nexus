const TOKEN = process.env.DISCORD_TOKEN;
const GUILD_ID = process.env.DISCORD_GUILD_ID || "1520375539351814204";

if (!TOKEN) {
  console.error("Error: DISCORD_TOKEN environment variable is required.");
  process.exit(1);
}

const API = "https://discord.com/api/v10";
const headers = {
  "Authorization": `Bot ${TOKEN}`,
  "Content-Type": "application/json",
};

async function discordFetch(endpoint, options = {}) {
  const url = `${API}${endpoint}`;
  const res = await fetch(url, { ...options, headers: { ...headers, ...options.headers } });
  if (res.status === 204) return null;
  const data = await res.json().catch(() => null);
  if (!res.ok) {
    throw new Error(`Discord API Error [${res.status}] ${endpoint}: ${JSON.stringify(data)}`);
  }
  return data;
}

async function main() {
  console.log(`Connecting to Discord Guild: ${GUILD_ID}...`);
  const channels = await discordFetch(`/guilds/${GUILD_ID}/channels`);
  console.log(`Found ${channels.length} existing channels/categories.`);

  const findChannel = (name, type) => channels.find(c => c.name.toLowerCase() === name.toLowerCase() && (type === undefined || c.type === type));

  // 1. Create or Find Categories
  const categoryDefs = [
    { name: "WELCOME & INFO", position: 0 },
    { name: "PROJECT PULSE", position: 1 },
    { name: "PERSONA LAB", position: 2 },
    { name: "COMMUNITY & DEV", position: 3 },
  ];

  const categoryMap = {};
  for (const cat of categoryDefs) {
    let existing = channels.find(c => c.type === 4 && c.name.toUpperCase() === cat.name);
    if (!existing) {
      console.log(`Creating category: ${cat.name}`);
      existing = await discordFetch(`/guilds/${GUILD_ID}/channels`, {
        method: "POST",
        body: JSON.stringify({ name: cat.name, type: 4, position: cat.position })
      });
    } else {
      console.log(`Found category: ${cat.name} (${existing.id})`);
    }
    categoryMap[cat.name] = existing.id;
  }

  // 2. Channels definitions
  const channelDefs = [
    // Welcome & Info
    { name: "rules", category: "WELCOME & INFO", topic: "NEXUS community rules, guidelines, and official links", type: 0 },
    { name: "getting-started", category: "WELCOME & INFO", topic: "Quick start guide: installation, hardware sizing, and first persona", type: 0 },
    { name: "announcements", category: "WELCOME & INFO", topic: "Official NEXUS announcements and major milestone drops", type: 0 },

    // Project Pulse
    { name: "changelog", category: "PROJECT PULSE", topic: "Official release notes and changelogs", type: 0, renameFrom: "updates" },
    { name: "github-feed", category: "PROJECT PULSE", topic: "Automated GitHub releases, commits, PRs, and issues feed", type: 0 },

    // Persona Lab
    { name: "share-a-persona", category: "PERSONA LAB", topic: "Share custom AI specialist personas and prompt templates", type: 0 },
    { name: "persona-ideas", category: "PERSONA LAB", topic: "Brainstorm new specialist roles, tool permissions, and workflows", type: 0 },

    // Community & Dev
    { name: "general", category: "COMMUNITY & DEV", topic: "General discussions, terminal workflows, and AI pair-programming", type: 0 },
    { name: "local-models", category: "COMMUNITY & DEV", topic: "Ollama, vLLM, hardware benchmarks, and VRAM sizing discussion", type: 0 },
    { name: "mcp-and-tools", category: "COMMUNITY & DEV", topic: "Claude Code, Gemini CLI, Kiro, Cursor & MCP server integrations", type: 0 },
  ];

  const channelIdMap = {};

  for (const def of channelDefs) {
    const parentId = categoryMap[def.category];
    let existing = channels.find(c => c.name.toLowerCase() === def.name.toLowerCase() && c.type === def.type);
    
    // Check renameFrom
    if (!existing && def.renameFrom) {
      existing = channels.find(c => c.name.toLowerCase() === def.renameFrom.toLowerCase());
    }

    if (existing) {
      console.log(`Updating channel: #${def.name} (${existing.id}) -> category ${def.category}`);
      const updated = await discordFetch(`/channels/${existing.id}`, {
        method: "PATCH",
        body: JSON.stringify({
          name: def.name,
          parent_id: parentId,
          topic: def.topic
        })
      });
      channelIdMap[def.name] = updated.id;
    } else {
      console.log(`Creating channel: #${def.name} under ${def.category}`);
      const created = await discordFetch(`/guilds/${GUILD_ID}/channels`, {
        method: "POST",
        body: JSON.stringify({
          name: def.name,
          type: def.type,
          parent_id: parentId,
          topic: def.topic
        })
      });
      channelIdMap[def.name] = created.id;
    }
  }

  // Move forum #issues under PROJECT PULSE if exists
  const issuesForum = channels.find(c => c.name === "issues" && c.type === 15);
  if (issuesForum && categoryMap["PROJECT PULSE"]) {
    console.log(`Moving forum #issues (${issuesForum.id}) to PROJECT PULSE`);
    await discordFetch(`/channels/${issuesForum.id}`, {
      method: "PATCH",
      body: JSON.stringify({ parent_id: categoryMap["PROJECT PULSE"] })
    });
  }

  // Delete old default "Text Channels" category if empty
  const oldTextCat = channels.find(c => c.type === 4 && c.name === "Text Channels");
  if (oldTextCat) {
    const remaining = channels.filter(c => c.parent_id === oldTextCat.id);
    if (remaining.length <= 1) { // only if all moved out
      console.log(`Cleaning up old category: Text Channels (${oldTextCat.id})`);
      await discordFetch(`/channels/${oldTextCat.id}`, { method: "DELETE" }).catch(() => null);
    }
  }

  // 3. Post Onboarding Embed into #rules
  const rulesId = channelIdMap["rules"];
  if (rulesId) {
    const existingMsgs = await discordFetch(`/channels/${rulesId}/messages?limit=5`);
    if (existingMsgs && existingMsgs.length === 0) {
      console.log(`Posting rules embed into #rules (${rulesId})...`);
      await discordFetch(`/channels/${rulesId}/messages`, {
        method: "POST",
        body: JSON.stringify({
          embeds: [
            {
              title: "⚡ Welcome to the NEXUS Community",
              description: "**Network of EXperts, Unified in Strategy**\n\nNEXUS is an open-source orchestration layer for AI coding CLIs. We bring structure, local hardware routing, and shared specialist personas to tools like Claude Code, Gemini CLI, Kiro, and Cursor.",
              color: 0x00B894, // Nexus Emerald
              fields: [
                {
                  name: "📌 Community Guidelines",
                  value: "• **Be collaborative**: We're building open tooling for developers.\n• **High-signal discussions**: Keep code, prompt experiments, and bug reports focused.\n• **Security first**: Never share API keys, private credentials, or secrets.",
                  inline: false
                },
                {
                  name: "🔗 Official Links",
                  value: "• **Website**: [nexus.codelogiic.com](https://nexus.codelogiic.com)\n• **GitHub Engine**: [canoo/agent-nexus](https://github.com/canoo/agent-nexus)\n• **Personas Vault**: [canoo/Nexus-Personas](https://github.com/canoo/Nexus-Personas)\n• **Documentation**: [Docs & Guides](https://nexus.codelogiic.com/docs/)",
                  inline: false
                },
                {
                  name: "🚀 Where to start?",
                  value: "Check out <#${channelIdMap['getting-started'] || rulesId}> for the 5-minute install and local model setup!",
                  inline: false
                }
              ],
              footer: {
                text: "Agent NEXUS · Built for the terminal",
                icon_url: "https://nexus.codelogiic.com/favicon.svg"
              }
            }
          ]
        })
      });
    }
  }

  // 4. Post Onboarding Embed into #getting-started
  const startId = channelIdMap["getting-started"];
  if (startId) {
    const existingMsgs = await discordFetch(`/channels/${startId}/messages?limit=5`);
    if (existingMsgs && existingMsgs.length === 0) {
      console.log(`Posting getting-started embed into #getting-started (${startId})...`);
      await discordFetch(`/channels/${startId}/messages`, {
        method: "POST",
        body: JSON.stringify({
          embeds: [
            {
              title: "🧭 Getting Started with NEXUS in 5 Minutes",
              description: "Follow these steps to set up NEXUS, route tasks to local Ollama models, and sync specialist personas across your AI CLIs.",
              color: 0x00D4AA, // Bright Mint/Emerald
              fields: [
                {
                  name: "1️⃣ One-Command Installation",
                  value: "```bash\ncurl -sSL https://raw.githubusercontent.com/canoo/agent-nexus/main/install.sh | bash\n```\nThis installs the pre-compiled binary and configures `~/.config/nexus`.",
                  inline: false
                },
                {
                  name: "2️⃣ Recommended Local Model Sizing",
                  value: "• **4 GB VRAM**: `qwen2.5-coder:1.5b` + `llama3.2:3b`\n• **8 GB VRAM**: `qwen2.5-coder:7b` + `llama3.1:8b`\n• **16 GB VRAM**: `qwen2.5-coder:7b` + `qwen2.5:14b`\n• **24 GB+ / Apple Silicon**: `qwen2.5-coder:14b` + `qwen2.5:32b`",
                  inline: false
                },
                {
                  name: "3️⃣ Explore Community Personas",
                  value: "Personas live in `~/.config/nexus/personas/`. Browse or submit community personas at [canoo/Nexus-Personas](https://github.com/canoo/Nexus-Personas)!",
                  inline: false
                },
                {
                  name: "💬 Need help or have feedback?",
                  value: "Drop your questions in <#${channelIdMap['local-models'] || startId}> or <#${channelIdMap['general'] || startId}>!",
                  inline: false
                }
              ],
              footer: {
                text: "NEXUS Onboarding Guide",
                icon_url: "https://nexus.codelogiic.com/favicon.svg"
              }
            }
          ]
        })
      });
    }
  }

  // 5. Setup Webhook in #github-feed
  const feedId = channelIdMap["github-feed"];
  let webhookUrl = null;
  if (feedId) {
    const webhooks = await discordFetch(`/channels/${feedId}/webhooks`);
    let feedHook = webhooks.find(w => w.name === "NEXUS GitHub");
    if (!feedHook) {
      console.log(`Creating GitHub webhook in #github-feed (${feedId})...`);
      feedHook = await discordFetch(`/channels/${feedId}/webhooks`, {
        method: "POST",
        body: JSON.stringify({ name: "NEXUS GitHub" })
      });
    }
    webhookUrl = `${feedHook.url}/github`;
    console.log(`\n🎉 GitHub Webhook URL ready:\n${webhookUrl}\n`);
  }

  return { channelIdMap, webhookUrl };
}

main().then(res => {
  console.log("Discord server setup complete!");
  process.exit(0);
}).catch(err => {
  console.error(err);
  process.exit(1);
});
