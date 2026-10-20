package config

const DefaultTOML = `# xis configuration.
# xis reads this file, then <repo>/.xis.toml, then XIS_* environment variables.
# Example: XIS_LLM_PROVIDER=none turns off the LLM layer for one command.
# See docs/CONFIG.md for every key.

[capture]
# Take a snapshot before each mutating tool call, not only after it.
precise = false
# Paths that snapshots never include. A name that ends in "/" is a directory.
exclude = [".env*", "*.pem", "*.key", "id_rsa*", "*.p12", "node_modules/", "vendor/", "dist/", "build/", "target/", ".venv/", "__pycache__/", ".next/"]
# Tools that can change files. xis takes a snapshot after each call.
# A name that ends in "*" matches a prefix.
mutating_tools = ["Edit", "Write", "MultiEdit", "NotebookEdit", "Bash", "mcp__*"]
# Tools that only read. xis records them without a snapshot.
readonly_tools = ["Read", "Grep", "Glob", "LS", "WebFetch", "WebSearch", "TodoWrite", "Task", "Agent", "ExitPlanMode", "BashOutput", "KillShell", "ToolSearch", "Skill"]
# Files larger than this (in bytes) get no line attribution. 1048576 = 1 MB.
max_file_bytes = 1048576
# xis truncates tool output to this size (in bytes) before storage. 32768 = 32 KB.
max_output_bytes = 32768
# Extra regular expressions for shell commands that run tests.
test_commands = []
# Extra glob patterns for test files.
test_globs = []

[llm]
# auto | anthropic | openaicompat | none. "auto" uses anthropic when the
# API key variable is set, and none when it is not set.
provider = "auto"
model = "claude-sonnet-5-5"
# openaicompat only. For Ollama, use "http://localhost:11434/v1".
base_url = ""
api_key_env = "ANTHROPIC_API_KEY"
# xis stops LLM analysis of one change when the estimated cost gets to this value (USD).
max_usd_per_change = 0.50
# If false, xis sends only metadata, paths and agent text. It never sends file contents.
send_code = true
# Prices (USD per 1 million tokens) for the cost estimate.
price_input_per_mtok = 3.0
price_output_per_mtok = 15.0
max_tokens = 4096

[rules]
# Rule IDs to turn off, for example ["LARGE_CHANGE"].
disabled = []
# Severity overrides, for example { OUT_OF_SCOPE = "low" }.
severity = {}
large_change_lines = 500
large_change_files = 20
retry_loop_runs = 4

[scope]
# Paths that are always in scope for OUT_OF_SCOPE. Put this in <repo>/.xis.toml.
paths = []

[redact]
# Extra regular expressions for secrets.
patterns = []
# Minimum Shannon entropy (bits per character) for a high-entropy token.
entropy_threshold = 4.5
# Minimum token length (characters) for the entropy check.
entropy_min_length = 20

[retention]
# xis deletes snapshots of sessions older than this (days).
snapshots_days = 30
# xis deletes spool files that ingest could not read after this (days).
raw_days = 7

[notes]
# If true, "xis precommit install" also installs a post-commit hook that runs "xis note write".
auto = false

[ui]
theme = "auto"
# Key overrides, for example { quit = "ctrl+c" }.
keymap = {}
`
