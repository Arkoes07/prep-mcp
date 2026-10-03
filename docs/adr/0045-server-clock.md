# Server clock
- `read.Reader.Now()` is the read side's clock. Production passes `time.Now`, tests pass a fixed day. `get_context` uses it only when the caller gives no `date`.
- `server.New(r, w, cfg, version)` takes the `*read.Reader` and the `*write.Writer`. The writer has its own clock, injected the same way, and uses it for `created`, `updated` and the `{date}` default.
