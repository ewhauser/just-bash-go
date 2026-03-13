import { ToolLoopAgent, createAgentUIStreamResponse, stepCountIs } from "ai";
import { createBashTool } from "bash-tool";
import { existsSync } from "fs";
import { Bash, OverlayFs } from "just-bash";
import { dirname, join } from "path";
import { fileURLToPath } from "url";

const __dirname = dirname(fileURLToPath(import.meta.url));
const AGENT_DATA_DIR = join(__dirname, "./_agent-data");

const SYSTEM_INSTRUCTIONS = `You are an expert on gbash, a deterministic, sandbox-only, bash-like runtime for AI agents implemented in Go.

You have access to a bash sandbox with the full source code of:
- gbash/ - The main Go repository, including the browser website integration

Refer to the repository README.md and source files when answering. Focus on how gbash works, how to embed it, the available commands, its filesystem model, and how the website integration is wired together.

Use the sandbox to explore the source code, demonstrate commands, and help users understand:
- How to use gbash
- The implementation details of gbash
- The browser/WASM website integration

Use cat to read files. Use head, tail to read parts of large files.
Keep responses concise.`;

export async function POST(req: Request) {
  if (!existsSync(AGENT_DATA_DIR)) {
    return new Response(
      "Agent backend unavailable: run `pnpm sync-agent-data` before starting the app.\n",
      { status: 503 }
    );
  }
  if (!process.env.ANTHROPIC_API_KEY) {
    return new Response(
      "Agent backend unavailable: set ANTHROPIC_API_KEY for the server runtime.\n",
      { status: 503 }
    );
  }

  const { messages } = await req.json();
  const overlayFs = new OverlayFs({ root: AGENT_DATA_DIR, readOnly: true });
  const sandbox = new Bash({ fs: overlayFs, cwd: overlayFs.getMountPoint() });
  const bashToolkit = await createBashTool({
    sandbox,
    destination: overlayFs.getMountPoint(),
  });

  // Create a fresh agent per request for proper streaming
  const agent = new ToolLoopAgent({
    model: process.env.AI_MODEL ?? "claude-haiku-4-5",
    instructions: SYSTEM_INSTRUCTIONS,
    tools: {
      bash: bashToolkit.tools.bash,
    },
    stopWhen: stepCountIs(20),
  });

  return createAgentUIStreamResponse({
    agent,
    uiMessages: messages,
  });
}
