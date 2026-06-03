# QwenPortal

轻量级 LLM API 统一代理网关。通过单一端口暴露 OpenAI/Claude 兼容接口，支持自定义 Provider 管理、多模型路由、流量统计和 Web 管理界面。

## 架构

```
┌─────────────────────────────────────┐
│         客户端 (curl/SDK)            │
└──────────┬──────────────────────────┘
           │ :8080
┌──────────▼──────────────────────────┐
│         QwenPortal (Go)             │
│  ┌──────────────────────────────┐   │
│  │  /v1/chat/completions        │   │
│  │  /v1/embeddings              │   │  OpenAI 兼容
│  │  /v1/models                  │   │
│  │  /v1/messages                │   │  Claude 兼容
│  ├──────────────────────────────┤   │
│  │  /admin/api/*                │   │  REST 管理 API
│  ├──────────────────────────────┤   │
│  │  /admin/*  →  Flask Web UI   │   │  管理界面
│  └──────────────────────────────┘   │
│                    │                │
│         SQLite ◄───┘                │
└─────────────────────────────────────┘
         │                    │
    OpenAI 兼容           Anthropic 兼容
     Provider              Provider
```

## 特性

- **统一网关** — 单端口 (8080) 提供所有服务，无需记忆多个地址
- **多 Provider 管理** — 通过 API 或 Web UI 增删改查 Provider，支持 OpenAI / Anthropic 兼容接口
- **智能路由** — 根据模型名称精确/前缀/通配符匹配到对应 Provider，增删 Provider 自动刷新
- **Web 管理界面** — 仪表盘 (请求量图表 + 统计卡片)、Provider 管理、API Key 管理
- **Provider 工具** — 一键 "Fetch Models" 拉取 Provider 模型列表、"Test Connection" 测试连通性
- **无需 API Key 即可访问模型代理接口** — 开箱即用，管理接口默认放行 localhost
- **CORS 支持** — 浏览器端直接调用
- **原始 API Key 遮蔽** — GET 响应中自动遮蔽 Key，编辑时自动保留未修改的 Key
- **SHA-256 哈希存储** — API Key 经哈希后存入数据库
- **流式 (SSE) 透传** — 完整支持 streaming 模式

## 快速开始

### 1. 构建

```bash
go build -o qwenportal.exe ./cmd/qwenportal/
```

### 2. 运行

```bash
./qwenportal.exe
```

首次运行自动生成管理员 Key，输出到控制台并保存到 `data/admin_key.txt`。

### 3. 打开 Web 界面

浏览器访问 [http://localhost:8080](http://localhost:8080)

### 4. 添加 Provider

通过 Web UI 添加你的 Provider（如 Qwen、DeepSeek、OpenAI 等）：
- 点 "Add Provider"
- 填写名称、Base URL、API Key
- 点 "Fetch from Provider" 自动拉取模型列表
- 点 "Test Connection" 验证连通性
- 保存

### 5. 使用 API

无需 Key，直接调用：

```bash
curl -X POST http://localhost:8080/v1/chat/completions \
  -d '{"model":"qwen3.6-plus","messages":[{"role":"user","content":"hello"}]}'
```

Python OpenAI SDK：

```python
from openai import OpenAI
client = OpenAI(base_url="http://localhost:8080/v1")
resp = client.chat.completions.create(
    model="qwen3.6-plus",
    messages=[{"role": "user", "content": "hello"}]
)
print(resp.choices[0].message.content)
```

## API 文档

### 代理接口 (无需认证)

| 端点 | 方法 | 说明 |
|------|------|------|
| `/v1/models` | GET | 列出所有可用模型 |
| `/v1/chat/completions` | POST | OpenAI 兼容的对话补全 |
| `/v1/embeddings` | POST | OpenAI 兼容的向量嵌入 |
| `/v1/messages` | POST | Claude 兼容的消息接口 |

### 管理接口 (`/admin/api`)

localhost 请求自动放行，远程需在请求头添加 `Authorization: Bearer <admin-key>`。

| 端点 | 方法 | 说明 |
|------|------|------|
| `/providers` | GET | 列出所有 Provider |
| `/providers` | POST | 创建 Provider |
| `/providers/:id` | GET | 获取 Provider 详情 |
| `/providers/:id` | PUT | 更新 Provider |
| `/providers/:id` | DELETE | 删除 Provider |
| `/providers/fetch-models` | POST | 拉取 Provider 的模型列表 |
| `/providers/test` | POST | 测试 Provider 连通性 |
| `/keys` | GET | 列出 API Key |
| `/keys` | POST | 创建 API Key |
| `/keys/:id` | PUT | 更新 API Key |
| `/keys/:id` | DELETE | 删除 API Key |
| `/stats` | GET | 获取请求统计 (`?hours=24`) |

## 配置

编辑 `config.yaml`：

```yaml
server:
  host: "0.0.0.0"
  port: 8080

database:
  path: "data/qwenportal.db"

webui:
  enabled: true        # 是否启用 Web 管理界面
  python: "python"     # Python 解释器路径
  port: 0              # 0 = 自动选择可用端口

logging:
  level: "info"
```

## Provider 配置示例

| Provider | Base URL | 类型 |
|----------|----------|------|
| Qwen | `https://qwen.aikit.club/v1` | openai |
| DeepSeek | `https://api.deepseek.com/v1` | openai |
| OpenAI | `https://api.openai.com/v1` | openai |
| Anthropic | `https://api.anthropic.com/v1` | anthropic |

模型匹配支持三种模式：
- **精确匹配** — `gpt-4o` 只匹配该模型
- **前缀匹配** — `gpt-4*` 匹配所有 `gpt-4` 开头的模型
- **通配符** — `*` 匹配任意模型

## Web UI

| 路由 | 页面 |
|------|------|
| `/admin/dashboard` | 仪表盘：请求量趋势图 + 统计 |
| `/admin/providers` | Provider 列表管理 |
| `/admin/providers/add` | 添加 Provider |
| `/admin/providers/:id/edit` | 编辑 Provider |
| `/admin/keys` | API Key 管理 |

## 运行测试

```bash
# 启动服务器
./qwenportal.exe

# 运行工具调用测试（覆盖 16 个典型场景）
python test_agent_tools.py
```

## 项目结构

```
├── cmd/qwenportal/       # 入口
├── internal/
│   ├── api/              # HTTP 处理器 (admin, openai, claude)
│   ├── config/           # 配置加载
│   ├── db/               # SQLite 数据库层
│   ├── middleware/        # 认证、CORS、日志
│   ├── models/           # 数据模型
│   └── proxy/            # 转发引擎 + 路由
├── webui/                # Flask Web 管理界面
├── scripts/              # 部署脚本
├── .github/workflows/    # CI/CD (GitHub Actions)
└── test_agent_tools.py   # 16 项编程 Agent 工具调用测试
```

## 部署到测试机器

```bash
# 交叉编译 Linux 二进制，上传到测试机器，启动服务并运行测试
python scripts/deploy.py 192.168.31.233 hao 934142
```

一键完成：
1. **交叉编译** — `GOOS=linux GOARCH=amd64 go build`
2. **上传** — 通过 SCP 推送二进制、配置、Web UI 到远程机器
3. **启动** — 停止旧服务，启动新服务
4. **测试** — 添加 Provider，运行 16 项 Agent 工具调用测试
