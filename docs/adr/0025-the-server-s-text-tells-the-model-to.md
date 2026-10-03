# The server's text tells the model to write to the vault, not to its own memory (M2)
- Found in M2d: asked to "log this session", Claude Code saved a note to its own auto-memory and never called `create_doc`. The client's memory instructions outrank a tool description, and MCP tools are loaded on demand.
- Decided: the server `instructions` and the `create_doc` and `append_section` descriptions say to use the write tools, and not local memory or notes files, for anything that belongs in the vault. The words are domain-neutral. `TestTextSteersWritesToTheVault` guards them.
- Instead of: putting the rule in `examples/vault/CLAUDE.md`, which is data and only reaches the model if `get_context` runs first. The example can still name the tools.
- Cost: this steers but cannot force. The model can still ignore it, and the prompt naming the tool is the reliable fallback.
