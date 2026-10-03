# Go 1.25 minimum *(deviation)*
- Instead of: Go 1.22+ (PRD section 7).
- Why: both SDKs require it (go-sdk v1.8.0 declares `go 1.25.0`, mcp-go v1.1.1 `go 1.25.5`). It also gives the full `os.Root` API used for path confinement.
- Cost: nothing older than Go 1.25 can build it.
