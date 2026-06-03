# QwenPortal Development Rules

## Testing Requirements
- All modifications MUST be tested in the local development environment before deployment.
- After any Go code changes, run `go build ./...` to verify compilation.
- After any backend changes, restart both the Go server and Flask server to ensure changes take effect.
- Verify database changes by checking if new tables/columns are properly created and data is correctly stored/retrieved.
