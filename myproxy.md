# QwenPortal — LLM API Unified Proxy Gateway

> Agent: 仅在做架构/协议级改动时按需查阅对应章节，勿默认通读全文。

> 版本: v0.1 | 语言: Go 1.26 + Python Flask | 数据库: SQLite | 许可: 内部

---

## Table of Contents

1. [Detailed Requirements & Features](#1-detailed-requirements--features)
2. [System Architecture & Design](#2-system-architecture--design)
3. [CI/CD](#3-cicd)
4. [Rebuild Guide & Reference](#4-rebuild-guide--reference)

---

## 1. Detailed Requirements & Features

### 1.1 Problem Statement

Teams using multiple LLM providers (Qwen, DeepSeek, OpenAI, Anthropic Claude, Google Gemini) face these problems:

- **Fragmented endpoints** — each provider has its own URL, auth scheme, and request format
- **No unified observability** — token usage, latency, error rates scattered across provider dashboards
- **Provider management overhead** — rotating keys, adding/removing models, testing connectivity
- **Protocol incompatibility** — Gemini uses a different API structure than OpenAI, requiring client-side adaptation

QwenPortal solves all of the above with a single-port, single-format gateway.

### 1.2 Functional Requirements

#### FR1: Unified Proxy API (unauthenticated, public)

| ID | Endpoint | Protocol | Methods | Description |
|----|----------|----------|---------|-------------|
| FR1.1 | `/v1/models` | OpenAI | GET | List all available models across all active providers, formatted as OpenAI `/v1/models` response |
| FR1.2 | `/v1/chat/completions` | OpenAI | POST | Chat completion with full streaming (SSE) support |
| FR1.3 | `/v1/embeddings` | OpenAI | POST | Embedding generation, forwarded to upstream |
| FR1.4 | `/v1/messages` | Anthropic | POST | Claude-compatible `/v1/messages` endpoint |

#### FR2: Gemini Protocol Translation

- Accept OpenAI-format requests for Gemini models (e.g. `gemini-2.0-flash`)
- Translate `messages[]` → Gemini `contents[]`, `system` → `systemInstruction`, `max_tokens` → `generationConfig.maxOutputTokens`
- Translate Gemini responses back to OpenAI format: `candidates[]` → `choices[]`, `usageMetadata` → `usage`
- Handle streaming: Gemini SSE chunks → OpenAI `chat.completion.chunk` format in near-real-time
- Gemini API key passed as URL query parameter (`?key=xxx`) instead of HTTP header

#### FR3: Multi-Provider Management (Admin API)

| ID | Endpoint | Description |
|----|----------|-------------|
| FR3.1 | `GET/POST /admin/api/providers` | List all / create provider |
| FR3.2 | `GET/PUT/DELETE /admin/api/providers/:id` | Detail / update / delete provider |
| FR3.3 | `GET /admin/api/providers/export` | Export all providers with full (unmasked) API keys as JSON |
| FR3.4 | `POST /admin/api/providers/import` | Import providers from JSON backup (dedup by name, merge) |
| FR3.5 | `POST /admin/api/providers/fetch-models` | Fetch model list from upstream provider |
| FR3.6 | `POST /admin/api/providers/test` | Test provider connectivity with a minimal request |

Provider schema:
- `name` — display name
- `provider_type` — `openai`, `anthropic`, `gemini`
- `base_url` — upstream base URL (e.g. `https://api.openai.com/v1`)
- `api_key` — upstream API key (masked in GET, stored encrypted via SHA-256)
- `models` — JSON string array of model names/patterns
- `is_active` — boolean toggle
- `priority` — ordering for routing

#### FR4: API Key Management

| ID | Endpoint | Description |
|----|----------|-------------|
| FR4.1 | `GET/POST /admin/api/keys` | List all / create new key |
| FR4.2 | `PUT/DELETE /admin/api/keys/:id` | Update (name, active status, rate limit) / delete key |
| FR4.3 | Auto-bootstrap | First-run creates an `admin` key, saved to `data/admin_key.txt`, displayed on stderr |

Key properties:
- Generated with `sk-` prefix followed by 48 random hex characters
- SHA-256 hash stored in DB; plaintext returned **only at creation time**
- `key_prefix` (first 8 chars) stored for identification in list views
- `rate_limit_rpm` field stored but **not currently enforced** in middleware

#### FR5: Admin Authentication

- Localhost/private IP (`127.0.0.1`, `::1`, `192.168.*`, `10.*`) → auto-authenticated, no token required
- Remote access → `Authorization: Bearer <admin-key>` header required
- SHA-256 hash verification against `api_keys` table
- Proxy API (FR1) is **unauthenticated** by design

#### FR6: Request Logging & Statistics

| Feature | Description |
|---------|-------------|
| Per-request logging | UUID request ID, model name, prompt/completion/cache tokens, latency, status code, request/response summary |
| Statistics API | `GET /admin/api/stats?hours=24&model=` — aggregated stats |
| Per-model logs | `GET /admin/api/logs?model=&hours=&limit=` — raw request log entries |
| Stats computed | Total requests, error count/rate, P50/P95/P99 latency percentiles, per-model breakdown, hourly time-series by model |

#### FR7: Model Testing (Admin)

- `POST /admin/api/models/test` — batch test multiple models with SSE streaming results
- Test flow per model: send message → measure latency → capture token count → compute tokens/sec
- Results streamed as SSE events with intermediate deltas for real-time progress
- Supports `openai`, `anthropic`, and `gemini` provider types with type-specific request formatting

#### FR8: Web Management UI (Flask)

| Route | Page | Features |
|-------|------|----------|
| `/admin/dashboard` | Dashboard | 7 stat cards, per-model usage table (sortable), hourly request/Token stacked bar charts (Chart.js), request log panel |
| `/admin/providers` | Provider List | CRUD table, export/import buttons, inline status indicators |
| `/admin/providers/add` | Add Provider | Form with Fetch Models (opens modal with searchable multi-select) and Test Connection |
| `/admin/providers/:id/edit` | Edit Provider | Same form, masked key preserved, priority/active status |
| `/admin/models` | Model Testing | Grouped model list by provider, batch test with SSE streaming, live per-model results |
| `/admin/tools` | Training Timer | Start/stop timer with animated pulse, 7-day stacked bar chart |
| `/admin/api` | API Reference | Swagger UI loading from `openapi.json` |

#### FR9: Training Timer (Auxiliary Feature)

- Simple singleton timer stored in `training_records` table
- API: start (returns timer ID), stop (computes duration), stats (grouped by day, 7-day window), active (query current running status)
- Used for tracking pelvic floor training sessions (via chart display)

### 1.3 Non-Functional Requirements

| Requirement | Target |
|-------------|--------|
| Proxy request timeout | 5 minutes |
| Streaming support | Full SSE passthrough for all provider types |
| Token accounting accuracy | Prefer upstream `usage` field; fallback to content-length/4 estimation |
| Startup time | < 2 seconds on modern hardware |
| Memory footprint | < 50 MB idle |
| SQLite concurrency | `SetMaxOpenConns(1)` — sequential writes, acceptable for low-to-medium traffic |
| Single binary deployment | Go binary + Python Flask files; no external database server needed |

---

## 2. System Architecture & Design

### 2.1 High-Level Architecture

```
┌──────────────────────────────────────────────────────────────────┐
│                        Clients                                    │
│  curl / OpenAI SDK / Claude SDK / LangChain / Any OpenAI-compat  │
│                                                                  │
│    POST /v1/chat/completions { model: "qwen3.6-plus", ... }      │
│    POST /v1/messages { model: "claude-sonnet-4", ... }           │
└─────────────────────────────┬────────────────────────────────────┘
                              │ :8080
┌─────────────────────────────▼────────────────────────────────────┐
│                     QwenPortal (Go/Gin)                           │
│                                                                  │
│  ┌──────────┬──────────┬──────────────┬──────────────────────┐   │
│  │ Middleware│ CORS     │ RequestLogger│  AdminAuth            │   │
│  │ Pipeline │ (global) │ (global)     │  (/admin/api/* only)   │   │
│  └──────────┴──────────┴──────────────┴──────────────────────┘   │
│                                                                  │
│  ┌──────────────────────────────────────────────────────────┐    │
│  │              Route Handlers                               │    │
│  │                                                           │    │
│  │  ┌─────────────────┐  ┌──────────────┐  ┌─────────────┐  │    │
│  │  │ OpenAIHandler    │  │ClaudeHandler │  │GeminiHandler │  │    │
│  │  │ openai.go        │  │claude.go     │  │gemini.go     │  │    │
│  │  │ /v1/chat/complet │  │/v1/messages  │  │(delegated)   │  │    │
│  │  │ /v1/models       │  │              │  │              │  │    │
│  │  │ /v1/embeddings   │  │              │  │              │  │    │
│  │  └────────┬─────────┘  └──────┬───────┘  └──────┬───────┘  │    │
│  │           │                  │                  │          │    │
│  │  ┌────────▼──────────────────▼──────────────────▼───────┐  │    │
│  │  │              Proxy Layer                              │  │    │
│  │  │  ┌──────────────┐  ┌───────────────────────────────┐  │  │    │
│  │  │  │ Router       │  │ Forwarder                     │  │  │    │
│  │  │  │ (RWMutex)    │  │ • Auth injection              │  │  │    │
│  │  │  │ model→provider│  │ • SSE streaming passthrough   │  │  │    │
│  │  │  │ exact/prefix/ │  │ • Token capture & estimation  │  │  │    │
│  │  │  │ wildcard      │  │ • Request/response summary    │  │  │    │
│  │  │  └──────────────┘  └───────────────────────────────┘  │  │    │
│  │  └──────────────────────────────────────────────────────┘  │    │
│  │                                                           │    │
│  │  ┌────────────────────────────────────────────────────┐   │    │
│  │  │ Admin API /admin/api/*                             │   │    │
│  │  │ CRUD providers, keys, stats, logs, model tests,    │   │    │
│  │  │ training timer                                     │   │    │
│  │  └────────────────────────────────────────────────────┘   │    │
│  │                                                           │    │
│  │  ┌────────────────────────────────────────────────────┐   │    │
│  │  │ NoRoute → Flask Reverse Proxy                     │   │    │
│  │  │ /admin/* → http://127.0.0.1:5100+/admin/*          │   │    │
│  │  └────────────────────────────────────────────────────┘   │    │
│  │                                                           │    │
│  │              SQLite DB ◄──────────────────────           │    │
│  │              data/qwenportal.db                           │    │
│  └───────────────────────────────────────────────────────────┘    │
│                           │                    │                  │
┌───────────────────────────┴────┐  ┌───────────┴────────────────┐ │
│ OpenAI-compatible Providers    │  │ Anthropic/Gemini Providers  │ │
│ (Qwen, DeepSeek, OpenAI, etc)  │  │ (Claude, Gemini-native)     │ │
│ Bearer <token> auth            │  │ x-api-key / ?key=           │ │
└────────────────────────────────┘  └────────────────────────────┘  │
```

### 2.2 Technology Stack

| Layer | Technology | Version | Justification |
|-------|-----------|---------|---------------|
| Runtime | Go | 1.26.2 | Single-binary deployment, excellent concurrency, fast compilation |
| HTTP framework | gin-gonic/gin | v1.12.0 | High-performance, excellent middleware support, SSE-compatible |
| Database | modernc.org/sqlite | v1.50.1 | Pure Go (no CGO), zero-config, WAL mode, perfect for single-node |
| Logging | go.uber.org/zap | v1.28.0 | Structured, leveled, high-performance logging |
| Config | gopkg.in/yaml.v3 | v3.0.1 | YAML parsing for config.yaml |
| UUID | google/uuid | v1.6.0 | Request ID generation |
| Web UI | Python Flask | >=3.0 | Quick iteration for admin interface, Jinja2 templates |
| Frontend | HTMX 2.0 + Chart.js 4.4 | CDN | Dynamic UI without build step |
| API docs | Swagger UI 5.20 | CDN | Interactive API reference |
| Deployment | paramiko | Python | SSH/SFTP remote deployment |
| Integration tests | openai Python SDK | latest | Agent tool-calling scenario tests |

### 2.3 Directory Structure & Module Map

```
C:\Code\QwenPortal\
├── cmd/qwenportal/main.go      # Entry point, dependency injection, lifecycle
│
├── internal/
│   ├── api/
│   │   ├── admin.go            # AdminHandler: 969 lines — all admin endpoints
│   │   ├── openai.go           # OpenAIHandler: /v1/chat/completions, /v1/models, /v1/embeddings
│   │   ├── claude.go           # ClaudeHandler: /v1/messages
│   │   └── gemini.go           # GeminiHandler: OpenAI↔Gemini protocol translation
│   │
│   ├── config/
│   │   └── config.go           # Config struct + Load() from YAML
│   │
│   ├── db/
│   │   ├── sqlite.go           # DB init, schema migrations, WAL mode
│   │   ├── providers.go        # Provider CRUD
│   │   ├── apikeys.go          # API key CRUD + SHA-256 hash/verify
│   │   ├── logs.go             # Request log insert + stats aggregation queries
│   │   └── training.go         # Training timer CRUD
│   │
│   ├── middleware/
│   │   ├── auth.go             # AdminAuth: localhost bypass + Bearer verification
│   │   ├── cors.go             # CORS: Allow-Origin: *
│   │   └── logging.go          # RequestLogger: UUID, timing, async DB write
│   │
│   ├── models/
│   │   ├── provider.go         # Provider struct
│   │   ├── apikey.go           # ApiKey struct
│   │   └── request.go          # RequestLog + StatsResponse structs
│   │
│   └── proxy/
│       ├── router.go           # Router: in-memory model→provider mapping (RWMutex)
│       └── forwarder.go        # Forwarder: HTTP forwarding, SSE capture, token parsing
│
├── webui/
│   ├── app.py                  # Flask app: routes, API proxy calls
│   ├── requirements.txt        # Flask >=3.0
│   ├── static/
│   │   ├── style.css           # Empty (styles in base.html)
│   │   └── openapi.json        # OpenAPI 3.0.3 spec
│   └── templates/
│       ├── base.html           # Layout + CDN imports (HTMX, Chart.js, Swagger)
│       ├── dashboard.html      # Stats, charts, logs
│       ├── providers.html      # Provider CRUD list
│       ├── provider_form.html  # Add/Edit form with fetch-models modal
│       ├── models.html         # Batch model testing
│       ├── tools.html          # Training timer
│       └── api.html            # Swagger UI
│
├── scripts/
│   ├── deploy.py               # 9-step one-click deployment script
│   └── deploy233.py            # Enhanced deployment with flags
│
├── config.yaml                 # Runtime configuration
├── go.mod/go.sum               # Go dependencies
├── test_agent_tools.py         # 16-scenario integration tests
├── AGENTS.md                   # AI agent development rules
├── README.md                   # Project overview (Chinese + English)
└── RELEASE.md                  # Release notes & deployment info
```

### 2.4 Core Module Design

#### 2.4.1 Request Processing Pipeline

```
┌──────────┐   ┌──────────┐   ┌───────────────┐   ┌─────────────┐   ┌──────────────┐
│ Client   │──▶│ CORS()   │──▶│ RequestLogger │──▶│ Route Match │──▶│ Handler      │
│ Request  │   │ (global) │   │ (global)      │   │              │   │ (per-path)   │
└──────────┘   └──────────┘   └───────┬───────┘   └─────────────┘   └──────┬───────┘
                                       │                                     │
                              ┌────────▼────────┐                  ┌────────▼───────┐
                              │ Set LogEntry on │                  │ Router.Find    │
                              │ gin.Context:    │                  │ Provider(model)│
                              │ request_id,     │                  │ (RWMutex RLock)│
                              │ start_time      │                  │                │
                              └─────────────────┘                  │ 1. Exact match │
                                                                   │ 2. Prefix match│
                                                                   │ 3. Wildcard "*"│
                                                                   └───────┬───────┘
                                                                           │
                                                              ┌────────────▼──────────┐
                                                              │ Provider Found?       │
                                                              │  Yes → continue       │
                                                              │  No → 404 + error msg │
                                                              └────────────┬──────────┘
                                                                           │
                                                              ┌────────────▼──────────┐
                                                              │ Type-based dispatch   │
                                                              │                      │
                                                              │ openai / anthropic    │
                                                              │  → Forwarder.Forward  │
                                                              │  → Build upstream URL │
                                                              │  → Inject auth header │
                                                              │  → HTTP POST upstream │
                                                              │  → SSE/non-stream     │
                                                              │  → Parse tokens       │
                                                              │  → Set ctx variables  │
                                                              │                      │
                                                              │ gemini               │
                                                              │  → GeminiHandler     │
                                                              │  → translateOpenAI→  │
                                                              │    Gemini format     │
                                                              │  → Upstream call     │
                                                              │  → Translate back    │
                                                              └────────────┬──────────┘
                                                                           │
                                                              ┌────────────▼──────────┐
                                                              │ Response Middleware   │
                                                              │ (RequestLogger post)  │
                                                              │                      │
                                                              │ Extract ctx vars:    │
                                                              │  prompt_tokens       │
                                                              │  completion_tokens   │
                                                              │  input_cache_tokens  │
                                                              │  request_summary     │
                                                              │  response_summary    │
                                                              │                      │
                                                              │ go InsertRequestLog()│
                                                              │  → async goroutine   │
                                                              │  → SQLite INSERT     │
                                                              └──────────────────────┘
```

#### 2.4.2 Router — Model-to-Provider Resolution (`internal/proxy/router.go`)

```
type Router struct {
    mu        sync.RWMutex
    providers []models.Provider
}
```

**Refresh** — `Refresh()` loads all `is_active=true` providers from SQLite into memory. Called:
- On server startup
- After any provider CREATE / UPDATE / DELETE

**Matching algorithm** (in `FindProvider(model string)`):

```
1. Exact match: iterate all providers, check if model name equals any entry in p.Models
2. Prefix match: iterate all providers, check if any p.Model starts with model name
   (e.g. model="gpt-4o" matches pattern "gpt-4*")
3. Wildcard match: fall back to the first provider with "*" in its model list
4. No match: return error "no provider found for model: <model>"
```

**Concurrency**: `sync.RWMutex` — `FindProvider` acquires RLock (shared reads), `Refresh` acquires Lock (exclusive write). This avoids database queries on every request.

#### 2.4.3 Forwarder — HTTP Request Forwarding (`internal/proxy/forwarder.go`)

```
type Forwarder struct {
    client *http.Client    // 5-minute timeout
    logger *zap.Logger
}
```

**Forward flow**:

1. Read request body → parse `stream` flag → store `request_summary`
2. Build target URL: `strings.TrimRight(provider.BaseURL, "/") + path`
3. Inject authentication header:
   - `provider.ProviderType == "anthropic"` → `x-api-key` + `anthropic-version`
   - `provider.ProviderType == "gemini"` → handled in GeminiHandler (key as query param)
   - default (openai) → `Authorization: Bearer <key>`
4. Forward upstream headers (minus Authorization/Content-Type)
5. **Streaming mode**: wrap response reader in `sseWriter`:
   - Passthrough SSE data to client in real-time
   - Scan each `data: {...}` line for key `"usage"`
   - Capture last usage chunk → parse `prompt_tokens`, `completion_tokens`, `input_cache_tokens`
   - Accumulate `delta.content` for fallback token estimation
   - Fallback: `prompt_tokens = len(body)/4`, `completion_tokens = len(accumulated_content)/4`
6. **Non-streaming mode**: read full response body → parse `usage` JSON → set context variables → forward to client
7. Store `response_summary` from first choice content on context

**Token parsing hierarchy** (4-level fallback):

```
1. upstream response body → openAIResponse.Usage
2. streaming: last SSE chunk containing "usage" key
3. streaming: accumulated content length / 4 (chars-per-token estimate)
4. default: all zeroes
```

#### 2.4.4 Gemini Protocol Translation (`internal/api/gemini.go`)

**Request conversion** (`translateOpenAIToGemini`):

| OpenAI | Gemini |
|--------|--------|
| `messages[].role=system` content | `systemInstruction.parts[].text` |
| `messages[].role=user` content | `contents[]{role:"user", parts[{text}]}` |
| `messages[].role=assistant` content | `contents[]{role:"model", parts[{text}]}` |
| `max_tokens` | `generationConfig.maxOutputTokens` |
| `temperature` | `generationConfig.temperature` |
| `top_p` | `generationConfig.topP` |
| `stream=true` | Endpoint: `:streamGenerateContent?alt=sse` |

**Response conversion** (`translateGeminiToOpenAI`):

| Gemini | OpenAI |
|--------|--------|
| `candidates[].content.parts[].text` | `choices[].message.content` |
| `candidates[].finishReason` → mapped | `choices[].finish_reason` |
| `usageMetadata.promptTokenCount` | `usage.prompt_tokens` |
| `usageMetadata.candidatesTokenCount` | `usage.completion_tokens` |
| `candidatesTokensDetails[modality=THINK]` | `usage.completion_tokens_details.reasoning_tokens` |

**Streaming conversion** (`geminiStreamReader`):

Gemini streaming return format (SSE):
```
data: {"candidates":[{"content":{"parts":[{"text":"Hello"}]}}]}
data: {"candidates":[{"content":{"parts":[{"text":" world"}]}}]}
data: {"usageMetadata":{"promptTokenCount":10,"candidatesTokenCount":5}}
```

Each chunk is parsed, translated to OpenAI chunk format, and re-emitted:
```
data: {"id":"chatcmpl-...","object":"chat.completion.chunk","choices":[{"index":0,"delta":{"content":"Hello"}}]}
```

#### 2.4.5 Database Schema (`internal/db/sqlite.go`)

```sql
-- ============================================================
-- Table: providers
-- Stores upstream LLM provider configurations
-- ============================================================
CREATE TABLE IF NOT EXISTS providers (
    id            INTEGER PRIMARY KEY AUTOINCREMENT,
    name          TEXT NOT NULL,                   -- Display name (e.g. "Qwen", "DeepSeek")
    provider_type TEXT NOT NULL DEFAULT 'openai',   -- 'openai' | 'anthropic' | 'gemini'
    base_url      TEXT NOT NULL,                    -- Upstream API base URL
    api_key       TEXT NOT NULL DEFAULT '',          -- Upstream API key
    models_json   TEXT NOT NULL DEFAULT '[]',        -- JSON array of model names/patterns
    is_active     INTEGER NOT NULL DEFAULT 1,        -- 0=disabled, 1=enabled
    priority      INTEGER NOT NULL DEFAULT 0,        -- Lower = higher priority for routing
    created_at    DATETIME DEFAULT CURRENT_TIMESTAMP,
    updated_at    DATETIME DEFAULT CURRENT_TIMESTAMP
);

-- ============================================================
-- Table: api_keys
-- Stores admin API keys (SHA-256 hashed)
-- ============================================================
CREATE TABLE IF NOT EXISTS api_keys (
    id             INTEGER PRIMARY KEY AUTOINCREMENT,
    name           TEXT NOT NULL,                   -- Key name (e.g. "admin", "dev-key")
    key_prefix     TEXT NOT NULL,                   -- First 8 chars of raw key for identification
    key_hash       TEXT NOT NULL UNIQUE,             -- SHA-256 hex digest of the raw key
    is_active      INTEGER NOT NULL DEFAULT 1,       -- 0=revoked, 1=active
    rate_limit_rpm INTEGER NOT NULL DEFAULT 0,       -- Rate limit (not enforced yet)
    created_at     DATETIME DEFAULT CURRENT_TIMESTAMP
);

-- ============================================================
-- Table: request_logs
-- Stores per-request telemetry
-- ============================================================
CREATE TABLE IF NOT EXISTS request_logs (
    id               INTEGER PRIMARY KEY AUTOINCREMENT,
    request_id       TEXT NOT NULL,                   -- UUID v4
    api_key_id       INTEGER,                         -- FK to api_keys (nullable for proxy)
    provider_id      INTEGER,                         -- FK to providers (nullable)
    model            TEXT NOT NULL DEFAULT '',         -- Model name used
    request_type     TEXT NOT NULL DEFAULT 'chat',     -- 'chat' | 'embedding' | 'message'
    prompt_tokens    INTEGER NOT NULL DEFAULT 0,
    completion_tokens INTEGER NOT NULL DEFAULT 0,
    input_cache_tokens INTEGER NOT NULL DEFAULT 0,    -- Added via ALTER TABLE migration
    latency_ms       INTEGER NOT NULL DEFAULT 0,
    status_code      INTEGER NOT NULL DEFAULT 200,
    is_error         INTEGER NOT NULL DEFAULT 0,       -- status_code >= 400
    request_summary  TEXT DEFAULT '',                  -- First user message content (truncated 200)
    response_summary TEXT DEFAULT '',                  -- First choice content (truncated 200)
    created_at       DATETIME DEFAULT CURRENT_TIMESTAMP
);

-- Indexes for log queries
CREATE INDEX IF NOT EXISTS idx_request_logs_created  ON request_logs(created_at);
CREATE INDEX IF NOT EXISTS idx_request_logs_provider ON request_logs(provider_id);
CREATE INDEX IF NOT EXISTS idx_request_logs_model    ON request_logs(model);

-- ============================================================
-- Table: training_records (auxiliary)
-- Simple start/stop timer for training sessions
-- ============================================================
CREATE TABLE IF NOT EXISTS training_records (
    id               INTEGER PRIMARY KEY AUTOINCREMENT,
    tool             TEXT NOT NULL DEFAULT 'pelvic_floor',  -- Tool category
    started_at       INTEGER NOT NULL,                      -- Unix epoch seconds
    ended_at         INTEGER,                               -- Unix epoch seconds (NULL=active)
    duration_seconds INTEGER NOT NULL DEFAULT 0,
    note             TEXT DEFAULT ''
);
```

#### 2.4.6 Flask Web UI Integration

**Startup sequence** (`main.go`):

```
1. Load config.yaml
2. Init zap logger
3. Init SQLite (create DB, run migrations)
4. Bootstrap admin key (first-run detection via db.ListApiKeys)
5. Create Router → Refresh() → Load providers into memory
6. Create Forwarder, OpenAIHandler, GeminiHandler, ClaudeHandler, AdminHandler
7. Setup Gin routes (proxy + admin + NoRoute)
8. Find available port (5100-5200) or use configured port
9. Fork Flask subprocess: exec.Command("python", "app.py") with env FLASK_PORT=<port>
10. Start HTTP server on configured address
11. Block on signal (SIGINT/SIGTERM)
12. On shutdown: SIGINT → 500ms → Kill Flask → server.Shutdown
```

**Reverse proxy**:
- Gin `NoRoute` handler catches all `/admin/*` requests
- Forwards to `http://127.0.0.1:<flaskPort>/admin/...` using `http.DefaultClient`
- Passes through all headers, status code, and body
- Flask callback pattern: Go admin API called via `http://127.0.0.1:8080/admin/api/...`

#### 2.4.7 Admin Authentication (`internal/middleware/auth.go`)

```
AdminAuth() gin.HandlerFunc:
    1. Extract "Bearer <token>" from Authorization header
    2. If present, verify SHA-256 hash against api_keys table
       → success: set ctx "api_key" and Next()
    3. If no token or invalid:
       a. Check client IP: 127.0.0.1, ::1, 192.168.*, 10.*
          → match: Next() (auto-authenticated)
       b. Otherwise: Abort 401 "authentication required"
```

### 2.5 Key Design Decisions

| Decision | Rationale | Trade-off |
|----------|-----------|-----------|
| **SQLite (no PostgreSQL)** | Zero ops, single file, no external dependency, modernc.org is pure Go (no CGO) | `SetMaxOpenConns(1)` limits concurrency; not suitable for >100 req/s sustained |
| **In-memory route cache** | Avoids per-request DB lookup; RWMutex provides concurrent-safe reads | Stale data window (max ~1ms) between DB write and Router.Refresh |
| **Flask as child process** | Quick UI iteration with Jinja2 templates; no frontend build pipeline | Additional process management; synchronous Python blocks on admin operations |
| **Proxy API unauthenticated** | Zero-friction for SDK users; API keys already controlled by upstream providers | No auth layer for proxy; relies on network-level isolation in production |
| **Async logging goroutine** | Non-blocking request path; typical INSERT latency ~1-5ms | Logs lost on crash before goroutine completes |
| **Token estimation fallback** | Provides useful metrics even when upstream doesn't return usage | Estimated values can be off by 2-3x for non-English text |
| **SHA-256 key hashing** | Even with full DB access, plaintext keys cannot be recovered | Key returned only at creation; must be saved immediately |
| **Three-level model matching** | Flexible routing without complex regex | Order-dependent for prefix/wildcard; first match wins |
| **Single port (8080)** | Simple deployment, one firewall rule, no reverse proxy needed | All services share same port; Flask admin accessible via same endpoint |

### 2.6 Request Flow Example (OpenAI Chat Completion)

```
Client: POST /v1/chat/completions
Body: {"model":"qwen3.6-plus", "messages":[{"role":"user","content":"Hello"}], "stream":true}
       │
       ▼
CORS Middleware: Set Allow-Origin: *
       │
       ▼
RequestLogger: Create LogEntry{RequestID: "uuid-...", StartTime: now}
       │
       ▼
OpenAIHandler.ChatCompletions:
  1. Read body → unmarshal → extract model="qwen3.6-plus"
  2. Router.FindProvider("qwen3.6-plus")
     → RLock providers, iterate:
       - Provider[0] name="QwenTest", models=["qwen3.6-plus"]
       - Exact match! Return Provider{BaseURL: "https://qwen.aikit.club/v1", ...}
  3. Set gin context: provider_id, log_entry.model
  4. provider.ProviderType == "openai" (not gemini)
  5. Forwarder.Forward(ctx, provider, "/chat/completions")
     a. Read body → detect stream=true
     b. targetURL = "https://qwen.aikit.club/v1/chat/completions"
     c. New HTTP request: POST targetURL, body=original, 
        Header: Authorization=Bearer <key>, Content-Type=application/json
     d. httpClient.Do(req) → upstream response
     e. Content-Type = "text/event-stream" → streaming mode
     f. Create sseWriter{c.Writer}
     g. io.Copy(sw, resp.Body):
        - Each SSE line written to client in real-time
        - Scan for "usage" key in data lines →
          capture last one
        - Accumulate delta.content → strings.Builder
     h. After copy: parseTokens(sw.lastUsage)
        → prompt_tokens=50, completion_tokens=10
        → Set ctx: proxy_prompt_tokens=50, proxy_completion_tokens=10
       │
       ▼
RequestLogger (after-handle):
  1. Extract ctx variables: tokens, model, status code, latency
  2. Build RequestLog struct
  3. go InsertRequestLog(&reqLog)  ← async goroutine
  4. Zap log: "request" uuid, method, path, status, latency, model, tokens
       │
       ▼
Response already streamed to client by sseWriter
```

---

## 3. CI/CD

### 3.1 Current Status

**No CI/CD pipeline configured.** The `.github/workflows/` directory exists but is empty.

All deployment is performed manually via deployment scripts.

### 3.2 Manual Build

```powershell
# Windows (native)
go build -o qwenportal.exe ./cmd/qwenportal/

# Linux cross-compilation (from Windows PowerShell)
$env:GOOS="linux"; $env:GOARCH="amd64"
go build -o qwenportal_linux ./cmd/qwenportal/

# Verification
go vet ./...
```

### 3.3 Manual Deployment

**Script**: `python scripts/deploy.py [host] [user] [password]`

The script performs these steps sequentially:

| Step | Action | Description |
|------|--------|-------------|
| 1 | Cross-compile Linux binary | `GOOS=linux GOARCH=amd64 go build` |
| 2 | SSH connect to remote | paramiko SSH client |
| 3 | Create remote directory | `mkdir -p /home/<user>/qwenportal/data` |
| 4 | Upload files | SFTP: binary, config.yaml (python→python3), webui/, test_agent_tools.py |
| 5 | Free port 8080 | Stop docker file-server, kill old process, `fuser -k`, wait up to 30s |
| 6 | Start service | `nohup ./qwenportal_linux > /tmp/qwenportal.log 2>&1 &` |
| 7 | Wait for readiness | Poll `GET /v1/models` up to 30 times (2s interval) |
| 8 | Add test provider | POST to `/admin/api/providers` with QwenTest config |
| 9 | Run test suite | `python3 test_agent_tools.py` with 16 scenarios |
| 10 | Restore Docker | `docker start file-server` |

**Enhanced script** (`deploy233.py`): adds `--skip-build`, `--skip-test`, `--skip-provider` flags.

### 3.4 Integration Tests

`test_agent_tools.py` — 16 scenario integration tests using the OpenAI Python SDK:

| # | Scenario | Coverage |
|---|----------|----------|
| 1 | Basic connectivity | `GET /v1/models` returns valid response |
| 2 | Code generation | Generate Python function via chat completion |
| 3 | Single-round tool call | Weather function calling (single call) |
| 4 | Multi-round tool call | Sequential tool calls resolving step-by-step |
| 5 | Streaming tool call | Tool calling with `stream=True` |
| 6 | Basic SSE streaming | Non-tool streaming output |
| 7 | Latency benchmark | Measure time-to-first-token (TTFT) and end-to-end latency |
| 8 | Concurrency | 3 simultaneous requests, all succeed |
| 9 | Error handling | Invalid model → proper error response |
| 10 | System prompt | Custom system instruction followed correctly |
| 11 | Tool error recovery | Tool returns error → model adapts |
| 12 | Multi-file project analysis | Read 3+ files and generate cross-file summary |
| 13 | Long context | 8K+ token input processed without truncation |
| 14 | Long conversation | 10+ rounds of back-and-forth conversation |
| 15 | Search-test loop | Generate code → save → run → fix if fails |
| 16 | Structured data toolchain | Create file → read → transform in pipeline |

Environment variables required:
- `PROXY_URL` (default: `http://localhost:8080/v1`)
- `MODEL` (default: `qwen3.6-plus`)

### 3.5 Recommended CI Pipeline

If GitHub Actions is configured, the following pipeline is recommended:

```yaml
# .github/workflows/ci.yml (not yet created)
name: QwenPortal CI

on:
  push:
    branches: [main, develop]
  pull_request:
    branches: [main]

jobs:
  lint-and-build:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
      - uses: actions/setup-go@v5
        with:
          go-version: '1.26'
      - run: go vet ./...
      - run: go build -o qwenportal_linux ./cmd/qwenportal/
      - run: go build -o qwenportal.exe ./cmd/qwenportal/

  unit-tests:
    runs-on: ubuntu-latest
    needs: lint-and-build
    steps:
      - uses: actions/checkout@v4
      - uses: actions/setup-go@v5
        with:
          go-version: '1.26'
      # TODO: write unit tests with Go standard testing + httptest
      # - run: go test ./... -v -coverprofile=coverage.out

  docker-build:
    runs-on: ubuntu-latest
    needs: unit-tests
    steps:
      - uses: actions/checkout@v4
      - uses: actions/setup-go@v5
        with:
          go-version: '1.26'
      - run: go build -o qwenportal_linux ./cmd/qwenportal/
      - uses: docker/setup-buildx-action@v3
      - uses: docker/login-action@v3
        with:
          registry: ghcr.io
          username: ${{ github.actor }}
          password: ${{ secrets.GITHUB_TOKEN }}
      # TODO: create Dockerfile and push image
      # - run: docker build -t ghcr.io/org/qwenportal:latest .
      # - run: docker push ghcr.io/org/qwenportal:latest

  integration-test:
    runs-on: ubuntu-latest
    needs: docker-build
    steps:
      - run: docker run -d -p 8080:8080 ghcr.io/org/qwenportal:latest
      - run: python -m pip install openai paramiko flask
      - run: sleep 3
      - run: curl -sf http://localhost:8080/v1/models
      # TODO: add test provider automatically
      # - run: python test_agent_tools.py
```

### 3.6 Dockerfile (Suggested)

```dockerfile
# Multi-stage build
FROM golang:1.26 AS builder
WORKDIR /build
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -o qwenportal ./cmd/qwenportal/

FROM python:3.11-slim
WORKDIR /app
COPY --from=builder /build/qwenportal .
COPY webui/ ./webui/
COPY config.yaml .
RUN pip install flask>=3.0 --no-cache-dir

EXPOSE 8080
CMD ["./qwenportal"]
```

---

## 4. Rebuild Guide & Reference

This section contains everything needed for another team to rebuild QwenPortal from scratch or extend it significantly.

### 4.1 Complete Rebuild from Scratch

#### Phase 1: Project Scaffolding

```bash
# Initialize Go module
mkdir qwenportal && cd qwenportal
go mod init github.com/user/qwenportal

# Install dependencies
go get github.com/gin-gonic/gin@v1.12.0
go get modernc.org/sqlite@v1.50.1
go get go.uber.org/zap@v1.28.0
go get gopkg.in/yaml.v3@v3.0.1
go get github.com/google/uuid@v1.6.0

# Create directory structure
mkdir -p cmd/qwenportal
mkdir -p internal/{api,config,db,middleware,models,proxy}
mkdir -p webui/{static,templates}
mkdir -p scripts
```

#### Phase 2: Implementation Order

This is the recommended build order, mirroring the actual development sequence:

| Step | Package | Files | What to implement |
|------|---------|-------|-------------------|
| 1 | `config` | `config.go` | Config struct + YAML loader with defaults |
| 2 | `models` | `provider.go`, `apikey.go`, `request.go` | Data structures for all entities |
| 3 | `db` | `sqlite.go` | DB init, WAL mode, schema migrations |
| 4 | `db` | `providers.go` | CRUD operations for providers |
| 5 | `db` | `apikeys.go` | CRUD + SHA-256 hash/verify for API keys |
| 6 | `proxy` | `router.go` | In-memory routing table with exact/prefix/wildcard matching |
| 7 | `middleware` | `cors.go` | CORS headers (Allow-Origin: *) |
| 8 | `middleware` | `auth.go` | Admin auth: localhost bypass + Bearer verification |
| 9 | `proxy` | `forwarder.go` | HTTP forwarding engine: auth injection, SSE passthrough, token parsing |
| 10 | `api` | `openai.go` | `/v1/chat/completions`, `/v1/embeddings`, `/v1/models` |
| 11 | `api` | `claude.go` | `/v1/messages` endpoint |
| 12 | `api` | `gemini.go` | OpenAI↔Gemini translation (request + response + streaming) |
| 13 | `api` | `admin.go` | All admin CRUD endpoints, model testing, stats, training |
| 14 | `middleware` | `logging.go` | Request logging: UUID, timing, async DB writes |
| 15 | `main.go` | - | Entry point: wire everything together, Flask subprocess, graceful shutdown |
| 16 | `webui` | `app.py`, templates | Flask admin interface |

#### Phase 3: Key Implementation Details

**Config struct** (`internal/config/config.go`):

```go
type Config struct {
    Server   ServerConfig   `yaml:"server"`
    Database DatabaseConfig `yaml:"database"`
    WebUI    WebUIConfig    `yaml:"webui"`
    Logging  LoggingConfig  `yaml:"logging"`
}

type ServerConfig struct {
    Host string `yaml:"host"`
    Port int    `yaml:"port"`
}

type DatabaseConfig struct {
    Path string `yaml:"path"`
}

type WebUIConfig struct {
    Enabled bool   `yaml:"enabled"`
    Python  string `yaml:"python"`
    Port    int    `yaml:"port"`
}

type LoggingConfig struct {
    Level string `yaml:"level"`
}
```

**API key format**: `sk-` + 48 random hex chars = 51 chars total.
```go
func generateAPIKey() (string, string) {
    bytes := make([]byte, 24)  // 24 bytes = 48 hex chars
    rand.Read(bytes)
    raw := "sk-" + hex.EncodeToString(bytes)
    hash := sha256Hex(raw)
    prefix := raw[:10]  // "sk-..." first 10 chars
    return raw, hash
}
```

**Dual Gin engine** (why not `gin.Default()`?): We use `gin.New()` + `gin.Recovery()` to avoid the default Logger middleware (we have our own RequestLogger).

**Flask integration pattern**:
```go
// find available port
func findAvailablePort() int {
    for port := 5100; port < 5200; port++ {
        addr := fmt.Sprintf("127.0.0.1:%d", port)
        ln, err := net.Listen("tcp", addr)
        if err == nil {
            ln.Close()
            return port
        }
    }
    return 5100
}

// Start Flask as child process
cmd := exec.Command(pythonPath, "app.py")
cmd.Dir = webUIDir
cmd.Env = append(os.Environ(), "FLASK_PORT="+strconv.Itoa(port))
cmd.Start()

// Reverse proxy: Gin NoRoute handler
r.NoRoute(func(c *gin.Context) {
    if strings.HasPrefix(c.Request.URL.Path, "/admin") {
        proxyToFlask(c, flaskPort)
        return
    }
    c.JSON(404, gin.H{"error": "not found"})
})
```

**SSE streaming token capture** pattern (in `forwarder.go`):

```go
type sseWriter struct {
    writer    io.Writer
    buf       []byte
    lastUsage []byte
    content   strings.Builder
}

func (w *sseWriter) Write(p []byte) (int, error) {
    w.buf = append(w.buf, p...)
    for {
        idx := bytes.IndexByte(w.buf, '\n')
        if idx < 0 { break }
        line := w.buf[:idx]
        w.buf = w.buf[idx+1:]
        line = bytes.TrimSpace(line)
        if bytes.HasPrefix(line, []byte("data: ")) {
            data := bytes.TrimSpace(line[6:])
            if bytes.Contains(data, []byte(`"usage"`)) {
                w.lastUsage = append([]byte{}, data...)
            }
            // Extract delta.content for fallback estimation
            // ... parse chunk JSON ...
        }
    }
    return w.writer.Write(p)
}
```

### 4.2 Adding a New Provider Type

To add support for a new LLM provider type (e.g. "azure", "cohere", "aws-bedrock"):

#### Step 1: New Handler File

Create `internal/api/<provider>.go` with a handler struct:

```go
type NewProviderHandler struct {
    forwarder *proxy.Forwarder
    router    *proxy.Router
    logger    *zap.Logger
}

func NewNewProviderHandler(f *proxy.Forwarder, r *proxy.Router, l *zap.Logger) *NewProviderHandler {
    return &NewProviderHandler{forwarder: f, router: r, logger: l}
}
```

#### Step 2: Request/Response Translation

If the provider uses a non-standard format, implement translation functions:
- Convert OpenAI request format to provider format
- Convert provider response back to OpenAI format
- Handle streaming conversion if applicable

#### Step 3: Register in `main.go`

```go
newProviderHandler := api.NewNewProviderHandler(forwarder, router_, logger)

// Register routes (if different from standard)
r.POST("/v1/new-provider-endpoint", newProviderHandler.Handler)
```

Or integrate into `OpenAIHandler.ChatCompletions` as a type check:

```go
if provider.ProviderType == "new-type" {
    c.Request.Body = io.NopCloser(strings.NewReader(string(body)))
    h.newProviderHandler.ChatCompletions(c)
    return
}
```

#### Step 4: Forwarder Auth Support

```go
// In forwarder.go, add auth header logic
if provider.ProviderType == "new-type" {
    req.Header.Set("X-API-Key", provider.APIKey)
    req.Header.Set("Provider-Auth-Version", "2024-01-01")
}
```

#### Step 5: Admin Handlers

Add branches in:
- `FetchProviderModels` — model list fetching
- `TestProvider` — connectivity test
- `TestModels` — batch test request formatting
- `extractContentFromBody` — response content extraction
- `parseTokensFromBody` — token parsing

#### Step 6: Web UI

Add provider type option in `templates/provider_form.html`.

### 4.3 Database Migration Strategy

SQLite schema migrations are handled in `sqlite.go:migrate()`:

```go
func migrate() error {
    // Phase 1: Create tables
    schemas := []string{...}
    for _, s := range schemas {
        DB.Exec(s)
    }

    // Phase 2: Column additions (safe to re-run)
    alterStmts := []string{
        `ALTER TABLE request_logs ADD COLUMN input_cache_tokens INTEGER NOT NULL DEFAULT 0`,
    }
    for _, s := range alterStmts {
        DB.Exec(s)  // Ignores "duplicate column" errors
    }

    // Phase 3: New tables
    DB.Exec(`CREATE TABLE IF NOT EXISTS new_feature (...)`)
}
```

**Guidelines for adding columns**:
1. Always use `INTEGER NOT NULL DEFAULT 0` for numeric columns
2. Always use `TEXT DEFAULT ''` for string columns
3. Ignore errors from `ALTER TABLE ADD COLUMN` (column may already exist)
4. Use `CREATE TABLE IF NOT EXISTS` for new tables

### 4.4 Configuration Reference

```yaml
server:
  host: "0.0.0.0"      # Listen address (use 127.0.0.1 for local-only)
  port: 8080            # Listen port

database:
  path: "data/qwenportal.db"  # Relative or absolute path to SQLite file

webui:
  enabled: true         # Set to false for headless deployment
  python: "python"      # "python3" on Linux, "python" on Windows
  port: 0               # 0=auto-select (5100-5200), >0 for fixed port

logging:
  level: "info"         # "debug" | "info" | "warn" | "error"
```

### 4.5 Provider Configuration Examples

| Name | Provider Type | Base URL | Example Model |
|------|--------------|----------|---------------|
| Qwen | `openai` | `https://qwen.aikit.club/v1` | `qwen3.6-plus` |
| DeepSeek | `openai` | `https://api.deepseek.com/v1` | `deepseek-chat` |
| OpenAI | `openai` | `https://api.openai.com/v1` | `gpt-4o` |
| Anthropic | `anthropic` | `https://api.anthropic.com/v1` | `claude-sonnet-4-20250514` |
| Gemini | `gemini` | `https://generativelanguage.googleapis.com/v1beta` | `gemini-2.0-flash` |

Model matching patterns:
- **Exact**: `gpt-4o` — matches only that exact model name
- **Prefix**: `gpt-4*` — matches `gpt-4o`, `gpt-4-turbo`, `gpt-4-32k`, etc.
- **Wildcard**: `*` — matches any model not already matched by another provider

### 4.6 Complete Admin API Reference

| Method | Endpoint | Auth | Request Body | Response |
|--------|----------|------|-------------|----------|
| GET | `/admin/api/providers` | Optional | — | `[Provider]` (keys masked) |
| POST | `/admin/api/providers` | Optional | `Provider` | `Provider` (keys masked) |
| GET | `/admin/api/providers/:id` | Optional | — | `Provider` |
| PUT | `/admin/api/providers/:id` | Optional | `Provider` | `Provider` |
| DELETE | `/admin/api/providers/:id` | Optional | — | `{"message":"deleted"}` |
| GET | `/admin/api/providers/export` | Optional | — | `[Provider]` (keys **unmasked**) |
| POST | `/admin/api/providers/import` | Optional | `{"providers":[Provider]}` | `{"imported":n,"updated":n,"skipped":n}` |
| POST | `/admin/api/providers/fetch-models` | Optional | `{"base_url","api_key","provider_type"}` | `{"models":["model1","model2"]}` |
| POST | `/admin/api/providers/test` | Optional | `{"base_url","api_key","provider_type","model"}` | `{"success":bool,"latency_ms":int}` |
| POST | `/admin/api/models/test` | Optional | `{"models":[],"message":"","timeout_seconds":30}` | SSE stream of test results |
| GET | `/admin/api/keys` | Optional | — | `[ApiKey]` |
| POST | `/admin/api/keys` | Optional | `{"name","rate_limit_rpm"}` | `ApiKey` (with raw `key_value`) |
| PUT | `/admin/api/keys/:id` | Optional | `{"name","is_active","rate_limit_rpm"}` | `{"message":"updated"}` |
| DELETE | `/admin/api/keys/:id` | Optional | — | `{"message":"deleted"}` |
| GET | `/admin/api/stats` | Optional | `?hours=24&model=` | `StatsResponse` |
| GET | `/admin/api/logs` | Optional | `?model=&hours=&limit=` | `[RequestLog]` |
| POST | `/admin/api/training/start` | Optional | `{"tool":"pelvic_floor"}` | `{"id":n,"started_at":"time"}` |
| POST | `/admin/api/training/stop` | Optional | `{"id":n}` | `{"message":"stopped"}` |
| GET | `/admin/api/training/stats` | Optional | `?tool=&days=7` | `[{"date","total_seconds","sessions"}]` |
| GET | `/admin/api/training/active` | Optional | `?tool=` | `{"active":bool,"id":n}` |

### 4.7 RequestLog Model (StatsResponse computed from this)

```go
type RequestLog struct {
    ID               int64     `json:"id"`
    RequestID        string    `json:"request_id"`
    ApiKeyID         *int64    `json:"api_key_id,omitempty"`
    ProviderID       *int64    `json:"provider_id,omitempty"`
    Model            string    `json:"model"`
    RequestType      string    `json:"request_type"`
    PromptTokens     int       `json:"prompt_tokens"`
    CompletionTokens int       `json:"completion_tokens"`
    InputCacheTokens int       `json:"input_cache_tokens"`
    LatencyMs        int64     `json:"latency_ms"`
    StatusCode       int       `json:"status_code"`
    IsError          bool      `json:"is_error"`
    RequestSummary   string    `json:"request_summary"`
    ResponseSummary  string    `json:"response_summary"`
    CreatedAt        time.Time `json:"created_at"`
}

type StatsResponse struct {
    TotalRequests    int                  `json:"total_requests"`
    ErrorCount       int                  `json:"error_count"`
    ErrorRate        float64              `json:"error_rate"`
    TotalPromptTokens int                 `json:"total_prompt_tokens"`
    TotalCompletionTokens int             `json:"total_completion_tokens"`
    AvgLatencyMs     float64              `json:"avg_latency_ms"`
    P50LatencyMs     float64              `json:"p50_latency_ms"`
    P95LatencyMs     float64              `json:"p95_latency_ms"`
    P99LatencyMs     float64              `json:"p99_latency_ms"`
    PerModel         []ModelStat          `json:"per_model"`
    Hourly           []HourlyStat         `json:"hourly"`
}

type ModelStat struct {
    Model                string  `json:"model"`
    RequestCount         int     `json:"request_count"`
    ErrorCount           int     `json:"error_count"`
    ErrorRate            float64 `json:"error_rate"`
    AvgLatencyMs         float64 `json:"avg_latency_ms"`
    TotalPromptTokens    int     `json:"total_prompt_tokens"`
    TotalCompletionTokens int    `json:"total_completion_tokens"`
    TokensPerSecond      float64 `json:"tokens_per_second"`
}

type HourlyStat struct {
    Hour            string `json:"hour"`
    RequestCount    int    `json:"request_count"`
    PromptTokens    int    `json:"prompt_tokens"`
    CompletionTokens int   `json:"completion_tokens"`
}
```

### 4.8 Go Module Dependencies

```
github.com/gin-gonic/gin v1.12.0       HTTP router + middleware
modernc.org/sqlite v1.50.1             Pure Go SQLite driver (no CGO)
go.uber.org/zap v1.28.0                Structured logging
gopkg.in/yaml.v3 v3.0.1                YAML config parser
github.com/google/uuid v1.6.0          UUID generation

Transitive (key):
  github.com/ncruces/go-strftime v1.0.0      Time formatting for SQLite
  github.com/quic-go/quic-go v0.59.0         QUIC transport (modernc dependency)
  modernc.org/libc v1.72.3                    Libc emulation (modernc runtime)
```

### 4.9 Known Limitations & Future Work

| Limitation | Impact | Suggested Fix |
|-----------|--------|---------------|
| `SetMaxOpenConns(1)` | Max ~100 req/s effective throughput | Use connection pool or connection per request for reads |
| No rate limit enforcement | `rate_limit_rpm` field stored but ignored | Implement in middleware using token-bucket algo |
| No proxy authentication | Anyone with network access can use gateway | Add optional Bearer token verification for proxy API |
| No unit tests | Only integration tests exist | Add Go `_test.go` files with `httptest` |
| No CI/CD pipeline | Manual deployment only | Create GitHub Actions workflow per Section 3.5 |
| Falcon/Streaming timeout | Upload/download large content drops after 5 min | Make forwarder timeout configurable per-provider |
| No TLS support | Traffic to proxy is unencrypted | Add `server.cert_file`/`server.key_file` config, wrap with `http.ListenAndServeTLS` |
| Hardcoded test credentials in deploy scripts | Security risk | Use environment variables or vault |
| Token estimation for non-English | Can be 2-3x off | Use provider's own tokenizer if available |
| Flask synchronous architecture | Admin UI blocks on long operations | Migrate to async Flask or replace with Go templates |
| Single admin key bootstrap | No multi-tenant support | Add admin user management |

### 4.10 Performance Characteristics

| Metric | Observed Value | Notes |
|--------|---------------|-------|
| Binary size (Linux) | ~25 MB | Go 1.26, CGO_ENABLED=0, stripped |
| Binary size (Windows) | ~30 MB | With debug symbols |
| Memory (idle) | ~15-25 MB | SQLite + in-memory provider cache |
| Memory (under load) | ~30-50 MB | Additional request buffers + goroutines |
| SQLite write latency | 1-5 ms | WAL mode, sequential writes |
| Forwarder overhead | <2 ms | Excluding upstream network latency |
| Startup time | <1 second | From binary launch to HTTP ready |
| Max concurrent requests | ~50 (practical) | Limited by SQLite single connection + goroutine scheduling |

### 4.11 Testing Guide

**Local development test flow**:

```bash
# 1. Build
go build -o qwenportal.exe ./cmd/qwenportal/

# 2. Start (requires config.yaml + webui/ in same directory)
./qwenportal.exe

# 3. Get admin key (displayed on first run, also in data/admin_key.txt)

# 4. Add a provider via curl
curl -X POST http://localhost:8080/admin/api/providers \
  -H "Content-Type: application/json" \
  -d '{
    "name": "TestProvider",
    "provider_type": "openai",
    "base_url": "https://api.openai.com/v1",
    "api_key": "sk-...",
    "models": ["gpt-4o-mini", "gpt-4*"],
    "is_active": true
  }'

# 5. Test proxy
curl -X POST http://localhost:8080/v1/chat/completions \
  -d '{"model":"gpt-4o-mini","messages":[{"role":"user","content":"Hello"}]}'

# 6. Run integration tests
pip install openai
$env:PROXY_URL="http://localhost:8080/v1"
$env:MODEL="gpt-4o-mini"
python test_agent_tools.py

# 7. Check stats
curl http://localhost:8080/admin/api/stats
```

### 4.12 Troubleshooting Common Issues

| Symptom | Likely Cause | Solution |
|---------|-------------|----------|
| `no provider found for model: X` | Model not in any provider's model list, or provider inactive | Check provider config in Web UI; verify model name spelling |
| `upstream request failed: connection refused` | Provider Base URL is wrong | Verify provider's URL is correct and the upstream service is running |
| `upstream returned 401` | Provider API key is invalid | Regenerate or update the API key |
| Flask web UI not loading (blank page or 502) | Flask process failed to start | Check if `python`/`python3` path in config.yaml is correct; check stderr output for Python errors |
| `address already in use` | Previous instance still running | Kill old process; on Windows use `netstat -ano \| findstr :8080` then `taskkill /PID <pid>` |
| `PRAGMA busy_timeout` or database locked | Concurrent SQLite write contention | `SetMaxOpenConns(1)` prevents this; if still occurs, check for lingering goroutines |
| Streaming stops mid-response | Upstream timeout or network issue | Check `timeout` setting in Forwarder (default 5 min); check network stability |
| Admin API returns 401 from localhost | Client IP not matching allowed patterns | Verify request is coming from `127.0.0.1` or `::1`; proxies may change the source IP |
