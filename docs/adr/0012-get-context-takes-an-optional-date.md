# `get_context` takes an optional `date` *(deviation)*
- Instead of: `get_context()` with no parameters, always using the server clock.
- Why: it lets the model or a human ask "what did week X look like", and lets demos run against the fictional 2026-W03 example. `date` defaults to the server's local today, so the normal call is still argument-free.
- Also decided: writes (`created`, `updated`, the `{date}` default) always use the server clock. No env override exists.
- Cost: one optional parameter. A model could pass a wrong date, which is why the default is the server clock rather than a required argument.
