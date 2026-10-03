# Config loaded once at startup
- Instead of: re-reading `prep.config.yaml` on every call, the way documents are.
- Why: an invalid config fails fast with every problem listed, rather than failing tool calls one by one.
- Cost: editing the config needs a server restart.
- Precedence: `--root` > `PREP_ROOT` > cwd, and `--config` > `PREP_CONFIG` > `<root>/prep.config.yaml`. A missing *default* config starts the server with no types, so reads work. A missing *explicit* config is fatal.
