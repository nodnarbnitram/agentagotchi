# Agentagotchi Status plugin

This personal plugin observes Codex lifecycle events and forwards a
content-free status event to the local Agentagotchi bridge.

The hook adapter intentionally discards prompts, commands, tool inputs,
tool responses, assistant messages, transcript paths, and full working
directory paths. If the bridge is unavailable, hooks return successfully
without changing Codex behavior.

After installation, open `/hooks` and trust all nine Agentagotchi hooks. Codex
skips installed and enabled hooks until their exact definitions are trusted.
If the desktop app was already open while trust was granted from the CLI, quit
and reopen it once.
