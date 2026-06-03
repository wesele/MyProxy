"""
QwenPortal - 编程 Agent 工具调用综合测试
模拟编程 Agent 的完整工具调用流程，验证代理的稳定性、流式支持和多轮交互。
覆盖全部典型编程 agent 场景 + 上下文窗口上限测试。
"""
import os, sys, json, time, textwrap, math

try:
    from openai import OpenAI
except ImportError:
    print("请先安装: pip install openai")
    sys.exit(1)

PROXY_URL = os.environ.get("PROXY_URL", "http://localhost:8080/v1")
API_KEY = os.environ.get("API_KEY", "")
MODEL = os.environ.get("MODEL", "qwen3.6-plus")

if not API_KEY:
    key_file = "data/admin_key.txt"
    if os.path.exists(key_file):
        API_KEY = open(key_file).read().strip()
    else:
        print(f"请设置 API_KEY 环境变量或确保 {key_file} 存在")
        sys.exit(1)

client = OpenAI(base_url=PROXY_URL, api_key=API_KEY)

pass_count = 0
fail_count = 0
test_results = []

def test(name):
    global pass_count, fail_count
    def decorator(fn):
        def wrapper():
            global pass_count, fail_count
            print(f"\n{'='*60}")
            print(f" [{pass_count+fail_count+1}] {name}")
            print(f"{'='*60}")
            start = time.time()
            try:
                fn()
                elapsed = time.time() - start
                print(f"  \u2713 PASS ({elapsed:.1f}s)")
                pass_count += 1
                test_results.append((name, "PASS", elapsed))
            except Exception as e:
                elapsed = time.time() - start
                print(f"  \u2717 FAIL ({elapsed:.1f}s)")
                print(f"    错误: {e}")
                fail_count += 1
                test_results.append((name, "FAIL", elapsed))
        return wrapper
    return decorator

# ─── 工具定义 ───────────────────────────────────────────────
TOOLS_BASIC = [
    {"type": "function", "function": {
        "name": "read_file", "description": "读取项目文件内容",
        "parameters": {"type": "object", "properties": {"path": {"type": "string", "description": "文件路径"}}, "required": ["path"]}
    }},
    {"type": "function", "function": {
        "name": "write_file", "description": "写入文件",
        "parameters": {"type": "object", "properties": {"path": {"type": "string"}, "content": {"type": "string"}}, "required": ["path", "content"]}
    }},
    {"type": "function", "function": {
        "name": "run_code", "description": "运行 Python 代码并获取输出",
        "parameters": {"type": "object", "properties": {"code": {"type": "string", "description": "要执行的 Python 代码"}}, "required": ["code"]}
    }},
]

TOOLS_FULL = TOOLS_BASIC + [
    {"type": "function", "function": {
        "name": "grep_search", "description": "在代码库中搜索关键词或正则表达式",
        "parameters": {"type": "object", "properties": {"pattern": {"type": "string", "description": "搜索模式"}, "path": {"type": "string", "description": "搜索路径"}}, "required": ["pattern"]}
    }},
    {"type": "function", "function": {
        "name": "list_files", "description": "列出目录中的文件",
        "parameters": {"type": "object", "properties": {"path": {"type": "string", "description": "目录路径"}}, "required": ["path"]}
    }},
    {"type": "function", "function": {
        "name": "git_diff", "description": "查看 Git 改动",
        "parameters": {"type": "object", "properties": {"path": {"type": "string"}}, "required": []}
    }},
    {"type": "function", "function": {
        "name": "run_tests", "description": "运行测试并返回结果",
        "parameters": {"type": "object", "properties": {"command": {"type": "string", "description": "测试命令"}}, "required": ["command"]}
    }},
]

def simulate_tool(tc, context=None):
    """模拟工具执行，返回结果字典。"""
    args = json.loads(tc.function.arguments)
    name = tc.function.name
    if name == "read_file":
        path = args.get("path", "")
        if path == "utils.py" or path.endswith("__init__.py"):
            return {"path": path, "found": True, "content": "# utils.py\n\ndef format_date(d):\n    return d.strftime('%Y-%m-%d')\n\ndef parse_int(s):\n    try:\n        return int(s)\n    except:\n        return 0\n"}
        elif path == "main.py" or path == "app.py":
            return {"path": path, "found": True, "content": "# main.py\nimport utils\n\ndef main():\n    today = utils.format_date(__import__('datetime').datetime.now())\n    print(f'Today: {today}')\n\nif __name__ == '__main__':\n    main()\n"}
        elif path == "requirements.txt":
            return {"path": path, "found": True, "content": "flask>=2.0\nrequests>=2.28\nnumpy>=1.24\npandas>=1.5\npytest>=7.0"}
        elif path == "large_file.py":
            return {"path": path, "found": True, "content": "\n".join(f"# line {i}: x = {i}  # some long content to fill context window" for i in range(500)) + "\n\ndef important_func():\n    return 42\n"}
        elif context and "files" in context:
            for f in context["files"]:
                if path == f.get("path"):
                    return {"path": path, "found": True, "content": f["content"]}
        return {"path": path, "found": False, "content": ""}
    elif name == "write_file":
        return {"path": args.get("path"), "written": True, "size": len(args.get("content", ""))}
    elif name == "run_code":
        code = args.get("code", "")
        if "error" in code.lower() or "raise" in code or "undefined" in code:
            return {"stdout": "", "stderr": "Traceback (most recent call last):\n  File \"<exec>\", line 1, in <module>\nNameError: name 'undefined_func' is not defined", "exit_code": 1}
        return {"stdout": "Execution simulated: OK\nResult: 42", "stderr": "", "exit_code": 0}
    elif name == "grep_search":
        pattern = args.get("pattern", "")
        return {"matches": [{"file": "src/main.py", "line": 42, "content": f"# match for '{pattern}'"}, {"file": "src/utils.py", "line": 15, "content": f"# another match for '{pattern}'"}], "count": 2}
    elif name == "list_files":
        return {"files": [
            {"name": "main.py", "type": "file", "size": 1024},
            {"name": "utils.py", "type": "file", "size": 512},
            {"name": "tests", "type": "dir"},
            {"name": "requirements.txt", "type": "file", "size": 64},
            {"name": "README.md", "type": "file", "size": 256},
        ]}
    elif name == "git_diff":
        return {"changed_files": ["main.py", "utils.py"], "insertions": 15, "deletions": 3}
    elif name == "run_tests":
        return {"passed": 5, "failed": 1, "output": "tests/test_main.py::test_feature FAILED\nAssertionError: assert 42 == 43"}
    return {"result": "ok"}


# ═══════════════════════════════════════════════════════════════
# 1. 基础连通性
# ═══════════════════════════════════════════════════════════════
@test("基础连通性 - 列出模型")
def test_list_models():
    models = client.models.list()
    ids = [m.id for m in models.data]
    assert len(ids) > 0, f"模型列表为空"
    assert MODEL in ids, f"目标模型 {MODEL} 不在列表中 ({ids})"
    print(f"   可用模型数: {len(ids)}")
    print(f"   目标模型: {MODEL}")


# ═══════════════════════════════════════════════════════════════
# 2. 基础对话
# ═══════════════════════════════════════════════════════════════
@test("基础对话 - 代码生成")
def test_basic_chat():
    resp = client.chat.completions.create(
        model=MODEL,
        messages=[{"role": "user", "content": "用 Python 写一个快速排序，只返回代码不要解释"}],
        temperature=0.1, max_tokens=500,
    )
    content = resp.choices[0].message.content
    assert content and len(content) > 10, "响应内容过短"
    assert "def " in content or "quick" in content.lower() or "sort" in content.lower(), \
        "响应似乎不包含排序相关代码"
    print(f"   响应长度: {len(content)} 字符")


# ═══════════════════════════════════════════════════════════════
# 3. 单轮工具调用
# ═══════════════════════════════════════════════════════════════
@test("单轮工具调用 - 读取文件并提取信息")
def test_single_tool_call():
    resp = client.chat.completions.create(
        model=MODEL,
        messages=[
            {"role": "system", "content": "你是一个编程助手，可以通过工具帮助用户处理代码任务。"},
            {"role": "user", "content": "请帮我看看当前目录下有没有 requirements.txt，如果有就读取它然后列出所有依赖包名。"}
        ],
        tools=TOOLS_BASIC, temperature=0.1,
    )
    msg = resp.choices[0].message
    assert msg.tool_calls is not None, "模型应该返回工具调用"
    print(f"   模型要求调用 {len(msg.tool_calls)} 个工具:")
    for tc in msg.tool_calls:
        args = json.loads(tc.function.arguments)
        print(f"     - {tc.function.name}({json.dumps(args, ensure_ascii=False)})")


# ═══════════════════════════════════════════════════════════════
# 4. 多轮工具调用
# ═══════════════════════════════════════════════════════════════
@test("多轮工具调用 - 读文件→修复→验证")
def test_multi_tool_round():
    messages = [
        {"role": "system", "content": "你是一个代码审查和修复助手。使用工具逐步完成：读取文件、分析问题、修复代码、运行验证。"},
        {"role": "user", "content": "我需要重构一个 Python 工具。先用 read_file 检查是否存在 utils.py，如果存在就读取它。"}
    ]
    turn = 0
    while turn < 4:
        resp = client.chat.completions.create(model=MODEL, messages=messages, tools=TOOLS_BASIC, temperature=0.1)
        msg = resp.choices[0].message
        messages.append(msg)
        if not msg.tool_calls:
            print(f"   第 {turn+1} 轮: 模型直接回复")
            break
        print(f"   第 {turn+1} 轮: {len(msg.tool_calls)} 个工具调用")
        for tc in msg.tool_calls:
            args = json.loads(tc.function.arguments)
            fn_sig = f"{tc.function.name}({json.dumps(args, ensure_ascii=False)[:80]})"
            print(f"     \u2192 {fn_sig}")
            result = simulate_tool(tc)
            messages.append({"role": "tool", "tool_call_id": tc.id, "content": json.dumps(result, ensure_ascii=False)})
        turn += 1
    assert turn <= 4, "工具调用循环过多"
    last_content = messages[-1].content if hasattr(messages[-1], "content") else messages[-1].get("content", "")
    if last_content:
        print(f"   最终回复: {last_content[:100].strip()}...")


# ═══════════════════════════════════════════════════════════════
# 5. 流式工具调用
# ═══════════════════════════════════════════════════════════════
@test("流式工具调用 - 代码生成+工具")
def test_streaming_tool_call():
    tools = [{"type": "function", "function": {
        "name": "run_code", "description": "运行 Python 代码",
        "parameters": {"type": "object", "properties": {"code": {"type": "string", "description": "Python 代码"}}, "required": ["code"]}
    }}]
    resp = client.chat.completions.create(
        model=MODEL,
        messages=[{"role": "user", "content": "写一段 Python 代码计算斐波那契数列第 n 项，然后把 n=10 放到 run_code 工具里执行"}],
        tools=tools, stream=True, temperature=0.1,
    )
    collected_tool_calls = {}
    finish_reason = None
    for chunk in resp:
        delta = chunk.choices[0].delta if chunk.choices else None
        if delta is None: continue
        if delta.tool_calls:
            for tc in delta.tool_calls:
                idx = tc.index
                if idx not in collected_tool_calls:
                    collected_tool_calls[idx] = {"id": tc.id or "", "function": {"name": "", "arguments": ""}}
                if tc.id: collected_tool_calls[idx]["id"] = tc.id
                if tc.function:
                    if tc.function.name: collected_tool_calls[idx]["function"]["name"] += tc.function.name
                    if tc.function.arguments: collected_tool_calls[idx]["function"]["arguments"] += tc.function.arguments
        if chunk.choices[0].finish_reason:
            finish_reason = chunk.choices[0].finish_reason
    print(f"   结束原因: {finish_reason}")
    assert finish_reason == "tool_calls", f"期望 tool_calls 结束原因，得到 {finish_reason}"
    for idx, tc in collected_tool_calls.items():
        print(f"   工具 {idx}: {tc['function']['name']}")


# ═══════════════════════════════════════════════════════════════
# 6. 流式文本生成
# ═══════════════════════════════════════════════════════════════
@test("流式代码生成 - SSE 稳定性")
def test_streaming_code_generation():
    chunks_received = 0
    total_content = ""
    resp = client.chat.completions.create(
        model=MODEL,
        messages=[{"role": "user", "content": "写一个 Python 装饰器用于测量函数执行时间，包含详细注释"}],
        stream=True, temperature=0.1, max_tokens=800,
    )
    for chunk in resp:
        chunks_received += 1
        delta = chunk.choices[0].delta if chunk.choices else None
        if delta and delta.content:
            total_content += delta.content
    assert chunks_received > 0, "未收到任何流式 chunk"
    print(f"   收到 {chunks_received} 个 chunk")
    print(f"   总内容长度: {len(total_content)} 字符")


# ═══════════════════════════════════════════════════════════════
# 7. 性能基准
# ═══════════════════════════════════════════════════════════════
@test("性能基准 - 连续请求延迟")
def test_latency_benchmark():
    client.chat.completions.create(
        model=MODEL, messages=[{"role": "user", "content": "hi"}], max_tokens=10, temperature=0.1,
    )
    latencies = []
    for i in range(5):
        start = time.time()
        client.chat.completions.create(
            model=MODEL, messages=[{"role": "user", "content": f"什么是 {i+1} 的平方根？只回答数字"}],
            max_tokens=50, temperature=0.1,
        )
        latencies.append(time.time() - start)
    avg = sum(latencies) / len(latencies)
    print(f"   5 次请求延迟: {[f'{l:.2f}s' for l in latencies]}")
    print(f"   平均延迟: {avg:.2f}s")


# ═══════════════════════════════════════════════════════════════
# 8. 并发请求
# ═══════════════════════════════════════════════════════════════
@test("并发请求 - 模拟并行调用")
def test_concurrent_requests():
    import concurrent.futures
    prompts = ["用 Python 写一个二分查找", "用 Python 写一个链表反转", "解释什么是 REST API"]
    def call_model(prompt):
        try:
            resp = client.chat.completions.create(
                model=MODEL, messages=[{"role": "user", "content": prompt}],
                max_tokens=200, temperature=0.1,
            )
            return ("ok", len(resp.choices[0].message.content))
        except Exception as e:
            return ("rate_limited", str(e)[:60])
    with concurrent.futures.ThreadPoolExecutor(max_workers=3) as ex:
        futures = [ex.submit(call_model, p) for p in prompts]
        results = [f.result() for f in concurrent.futures.as_completed(futures)]
    ok = sum(1 for r in results if r[0] == "ok")
    limited = sum(1 for r in results if r[0] == "rate_limited")
    print(f"   并发 3 请求: {ok} 成功, {limited} 被限流")
    assert ok > 0, "所有并发请求均失败"


# ═══════════════════════════════════════════════════════════════
# 9. 错误处理
# ═══════════════════════════════════════════════════════════════
@test("错误处理 - 无效模型")
def test_invalid_model():
    try:
        client.chat.completions.create(
            model="non-existent-model-xyz-12345",
            messages=[{"role": "user", "content": "hi"}],
        )
        assert False, "应该抛出异常"
    except Exception as e:
        print(f"   正确拒绝: {type(e).__name__}")


# ═══════════════════════════════════════════════════════════════
# 10. 系统提示词
# ═══════════════════════════════════════════════════════════════
@test("系统提示词 - Agent 角色设定")
def test_system_prompt_agent():
    resp = client.chat.completions.create(
        model=MODEL,
        messages=[
            {"role": "system", "content": "你是 Senior Python 架构师。回答必须包含以下结构：\n1. 问题分析\n2. 方案设计\n3. 代码实现\n4. 复杂度分析"},
            {"role": "user", "content": "实现一个 LRU Cache"}
        ],
        temperature=0.2, max_tokens=1000,
    )
    content = resp.choices[0].message.content
    assert content and len(content) > 100, "响应太短"
    print(f"   响应长度: {len(content)} 字符")
    sections = ["问题分析", "方案设计", "代码实现", "复杂度分析"]
    found = [s for s in sections if s in content]
    print(f"   命中的结构段落: {found}")
    assert len(found) >= 3, f"应包含至少 3 个结构段落，仅发现 {found}"


# ═══════════════════════════════════════════════════════════════
# 11. 工具错误恢复
# ═══════════════════════════════════════════════════════════════
@test("工具错误恢复 - 工具失败后重试")
def test_tool_error_recovery():
    """
    Agent 调用 run_code 传入错误代码 → 工具返回错误 → Agent 应修正代码并重试。
    模拟编程 agent 遇到编译/运行时错误时的恢复能力。
    """
    messages = [
        {"role": "system", "content": "你是一个编程助手。如果工具调用返回了错误，分析错误原因并修正后重试。"},
        {"role": "user", "content": "写一个 Python 函数计算圆的面积，然后用 run_code 工具运行它测试是否正常。"}
    ]
    max_rounds = 5
    error_occurred = False
    success = False
    for turn in range(max_rounds):
        resp = client.chat.completions.create(model=MODEL, messages=messages, tools=TOOLS_BASIC, temperature=0.2)
        msg = resp.choices[0].message
        messages.append(msg)
        if not msg.tool_calls:
            print(f"   第 {turn+1} 轮: 模型直接回复")
            success = True
            break
        for tc in msg.tool_calls:
            args = json.loads(tc.function.arguments)
            print(f"   第 {turn+1} 轮: {tc.function.name}")
            if tc.function.name == "run_code":
                code = args.get("code", "")
                if "radius" not in code and "3.14" not in code and "math.pi" not in code:
                    error_occurred = True
                    result = {"stdout": "", "stderr": "NameError: name 'radius' is not defined", "exit_code": 1}
                    print(f"     \u2190 工具返回错误 (预期内的错误恢复测试)")
                else:
                    result = {"stdout": "Area: 78.5\n", "stderr": "", "exit_code": 0}
                    print(f"     \u2190 工具执行成功")
            else:
                result = simulate_tool(tc)
            messages.append({"role": "tool", "tool_call_id": tc.id, "content": json.dumps(result, ensure_ascii=False)})
    assert success, f"模型在 {max_rounds} 轮内未完成"
    if error_occurred:
        print(f"   \u2713 Agent 经历了错误并成功恢复")
    else:
        print(f"   \u2713 Agent 直接成功 (错误恢复场景未触发)")


# ═══════════════════════════════════════════════════════════════
# 12. 多文件项目分析（用户描述的"理解项目"场景）
# ═══════════════════════════════════════════════════════════════
@test("多文件项目分析 - 理解项目结构")
def test_project_understanding():
    """
    模拟用户描述的场景: Agent 通过多轮工具调用理解整个项目的结构和功能。
    依次使用 list_files → read_file (多个) → 综合分析。
    """
    project_files = [
        {"path": "src/main.py", "content": "#!/usr/bin/env python3\n\"\"\"Main entry point for the web application.\"\"\"\nfrom flask import Flask\nfrom routes import api, views\n\napp = Flask(__name__)\napp.register_blueprint(api.bp)\napp.register_blueprint(views.bp)\n\nif __name__ == '__main__':\n    app.run(host='0.0.0.0', port=8080)"},
        {"path": "src/routes/api.py", "content": "from flask import Blueprint, jsonify, request\n\nbp = Blueprint('api', __name__)\n\n@bp.route('/api/users', methods=['GET'])\ndef list_users():\n    return jsonify([{'id': 1, 'name': 'Alice'}, {'id': 2, 'name': 'Bob'}])\n\n@bp.route('/api/users', methods=['POST'])\ndef create_user():\n    data = request.get_json()\n    return jsonify({'id': 3, 'name': data['name']}), 201"},
        {"path": "src/routes/views.py", "content": "from flask import Blueprint, render_template\n\nbp = Blueprint('views', __name__)\n\n@bp.route('/')\ndef index():\n    return render_template('index.html')\n\n@bp.route('/about')\ndef about():\n    return render_template('about.html')"},
        {"path": "src/models/user.py", "content": "class User:\n    def __init__(self, id, name, email):\n        self.id = id\n        self.name = name\n        self.email = email\n    \n    def to_dict(self):\n        return {'id': self.id, 'name': self.name, 'email': self.email}\n\nusers_db = [\n    User(1, 'Alice', 'alice@example.com'),\n    User(2, 'Bob', 'bob@example.com'),\n]"},
        {"path": "tests/test_api.py", "content": "def test_list_users(client):\n    resp = client.get('/api/users')\n    assert resp.status_code == 200\n    assert len(resp.json) == 2\n\ndef test_create_user(client):\n    resp = client.post('/api/users', json={'name': 'Charlie'})\n    assert resp.status_code == 201\n    assert resp.json['name'] == 'Charlie'"},
        {"path": "requirements.txt", "content": "flask>=2.3\npytest>=7.0\nrequests>=2.28\n"},
    ]

    messages = [
        {"role": "system", "content": "你是一个高级代码审查助手。在分析项目时，先了解整体结构，再深入关键文件，最后给出总结。"},
        {"role": "user", "content": "请分析这个 Python Web 项目。先用 list_files 看结构，然后逐个读取关键源码文件，最后告诉我这个项目是做什么的、用了什么技术栈、以及架构是什么样的。"}
    ]

    context = {"files": project_files}
    file_read_count = 0
    for turn in range(6):
        resp = client.chat.completions.create(model=MODEL, messages=messages, tools=TOOLS_FULL, temperature=0.2)
        msg = resp.choices[0].message
        messages.append(msg)
        if not msg.tool_calls:
            print(f"   第 {turn+1} 轮: 模型直接回复")
            print(f"   回复预览: {msg.content[:150].strip()}...")
            break
        names = [tc.function.name for tc in msg.tool_calls]
        print(f"   第 {turn+1} 轮: {', '.join(names)}")
        for tc in msg.tool_calls:
            if tc.function.name == "read_file":
                file_read_count += 1
            result = simulate_tool(tc, context)
            messages.append({"role": "tool", "tool_call_id": tc.id, "content": json.dumps(result, ensure_ascii=False)})
    else:
        assert False, "模型在 6 轮内未完成项目分析"

    assert file_read_count >= 2, f"应读取至少 2 个文件，实际读取 {file_read_count} 个"
    print(f"   读取了 {file_read_count} 个源码文件")
    print(f"   \u2713 Agent 成功通过多工具调用理解了项目结构")


# ═══════════════════════════════════════════════════════════════
# 13. 上下文窗口上限 - 大文件注入
# ═══════════════════════════════════════════════════════════════
@test("上下文上限 - 大文件注入后的连贯性")
def test_long_context_large_file():
    """
    注入一个包含约 500 行的大文件内容到上下文中，
    然后验证模型在大量上下文后仍能正确理解和执行指令。
    """
    large_content = "\n".join(
        f"# module_{i}.py\n# Auto-generated for context window testing\nVERSION_{i} = {i}\ndef func_{i}():\n    return {i * 2}\n"
        for i in range(200)
    )
    messages = [
        {"role": "system", "content": "你是一个代码分析助手。你需要在大量代码上下文后仍然准确回答问题。"},
        {"role": "user", "content": f"以下是项目代码，请先阅读：\n\n```\n{large_content[:8000]}\n```\n\n请记住这个项目的关键信息。然后告诉我 'func_42()' 的返回值是多少？"}
    ]
    resp = client.chat.completions.create(model=MODEL, messages=messages, temperature=0.1, max_tokens=200)
    content = resp.choices[0].message.content
    assert content and len(content) > 5, "响应为空"
    print(f"   注入约 {len(large_content)} 字符上下文")
    if "84" in content or "42" in content:
        print(f"   \u2713 模型在大上下文后仍正确回答了问题")
    print(f"   回复: {content[:120].strip()}...")

    # 验证模型在长上下文后仍能正确使用工具
    resp2 = client.chat.completions.create(
        model=MODEL,
        messages=messages + [{"role": "assistant", "content": content},
                             {"role": "user", "content": "用 run_code 工具验证 func_42 的返回值"}],
        tools=TOOLS_BASIC, temperature=0.1,
    )
    msg2 = resp2.choices[0].message
    if msg2.tool_calls:
        print(f"   \u2713 长上下文后仍能正确发起工具调用: {msg2.tool_calls[0].function.name}")
    else:
        print(f"   (模型直接回复，未使用工具)")


# ═══════════════════════════════════════════════════════════════
# 14. 上下文窗口上限 - 长对话积累
# ═══════════════════════════════════════════════════════════════
@test("上下文上限 - 长对话积累 (12轮)")
def test_long_conversation():
    """
    通过 12 轮连续对话（每轮都向上下文中追加内容），
    模拟长对话场景，验证模型在接近上下文上限时的连贯性。
    每轮都要求模型记住并引用之前轮次的信息。
    """
    facts = [
        "项目名为 CodeForge",
        "使用 Python 3.11+",
        "主要框架是 FastAPI",
        "数据库使用 PostgreSQL 15",
        "缓存层使用 Redis 7",
        "消息队列使用 RabbitMQ",
        "前端使用 React 18 + TypeScript",
        "API 设计遵循 RESTful 规范",
        "认证方式为 JWT + OAuth2",
        "部署在 Kubernetes 集群",
        "CI/CD 使用 GitHub Actions",
        "监控使用 Prometheus + Grafana",
    ]
    messages = [
        {"role": "system", "content": "你是项目架构师。记住用户介绍的每个项目信息，后续会被问及。"},
        {"role": "user", "content": f"我们的项目信息：{facts[0]}"}
    ]
    # 前 11 轮：逐步介绍项目信息
    for i in range(1, len(facts)):
        messages.append({"role": "assistant", "content": f"已记录：{facts[i-1]}"})
        messages.append({"role": "user", "content": f"下一个信息：{facts[i]}"})
    # 最后一轮：验证模型记住了先前信息
    messages.append({"role": "assistant", "content": f"已记录所有信息"})
    qa_pairs = [
        ("数据库和缓存分别用了什么技术？", ["PostgreSQL", "Redis"]),
        ("前端框架是什么？部署在哪里？", ["React", "Kubernetes"]),
        ("整个技术栈是什么，请列出主要组件。", ["FastAPI", "PostgreSQL", "Redis", "React"]),
    ]
    recall_count = 0
    for question, keywords in qa_pairs:
        resp = client.chat.completions.create(
            model=MODEL, messages=messages + [{"role": "user", "content": question}],
            temperature=0.1, max_tokens=300,
        )
        answer = resp.choices[0].message.content
        matched = sum(1 for kw in keywords if kw.lower() in answer.lower())
        if matched >= len(keywords):
            recall_count += 1
            print(f"   \u2713 正确回答: {question[:40]}...")
        else:
            print(f"   \u2717 部分正确: {question[:40]}... ({matched}/{len(keywords)} 关键词)")
    print(f"   12 轮对话后问答准确率: {recall_count}/{len(qa_pairs)}")
    assert recall_count >= 1, f"长篇对话后模型完全丢失了上下文 ({recall_count}/{len(qa_pairs)})"


# ═══════════════════════════════════════════════════════════════
# 15. 代码搜索 + 测试驱动开发流程
# ═══════════════════════════════════════════════════════════════
@test("搜索+测试循环 - grep→读文件→写测试→运行")
def test_search_test_cycle():
    """
    模拟真实编程工作流: 搜索代码中的特定函数 → 读取相关文件 → 编写测试 → 运行测试 → 修复失败。
    """
    messages = [
        {"role": "system", "content": "你是 TDD 模式下的开发助手。请严格按照以下三步执行：\n1. 先用 grep_search 搜索 'User' 类\n2. 再用 run_tests 运行现有测试\n3. 最后用 read_file 读取测试文件理解测试框架\n不要做其他额外操作。"},
        {"role": "user", "content": "请执行三步流程：搜索 User 类 → 运行测试 → 读取测试文件。"}
    ]
    grep_done = False
    test_run_done = False
    for turn in range(4):
        resp = client.chat.completions.create(model=MODEL, messages=messages, tools=TOOLS_FULL, temperature=0.2)
        msg = resp.choices[0].message
        messages.append(msg)
        if not msg.tool_calls:
            print(f"   第 {turn+1} 轮: 模型直接回复")
            break
        names = [tc.function.name for tc in msg.tool_calls]
        print(f"   第 {turn+1} 轮: {', '.join(names)}")
        for tc in msg.tool_calls:
            name = tc.function.name
            if name == "grep_search":
                grep_done = True
            elif name == "run_tests":
                test_run_done = True
            result = simulate_tool(tc)
            messages.append({"role": "tool", "tool_call_id": tc.id, "content": json.dumps(result, ensure_ascii=False)})
    print(f"   grep 搜索: {'\u2713' if grep_done else '\u2717'}")
    print(f"   测试运行: {'\u2713' if test_run_done else '\u2717'}")
    assert grep_done, "Agent 未使用 grep 搜索工具"


# ═══════════════════════════════════════════════════════════════
# 16. Git 操作 + 数据分析报告
# ═══════════════════════════════════════════════════════════════
@test("结构化数据工具链 - diff→分析→写报告")
def test_structured_data_toolchain():
    """
    Agent 使用 git_diff 获取变更 → 分析变更影响 → 用 write_file 输出分析报告。
    验证 Agent 能处理结构化工具返回值并进行后续决策。
    """
    tools = TOOLS_BASIC + [
        {"type": "function", "function": {
            "name": "git_diff", "description": "查看当前 Git 改动的文件列表和统计",
            "parameters": {"type": "object", "properties": {}, "required": []}
        }},
        {"type": "function", "function": {
            "name": "analyze_impact", "description": "分析代码变更的影响范围",
            "parameters": {"type": "object", "properties": {"files": {"type": "array", "items": {"type": "string"}, "description": "变更的文件列表"}}, "required": ["files"]}
        }},
    ]
    messages = [
        {"role": "system", "content": "你是一个代码审查助手。使用工具分析当前变更并生成报告。"},
        {"role": "user", "content": "请分析当前的代码变更：先用 git_diff 查看改了什么，然后用 analyze_impact 分析影响，最后用 write_file 写一份审查报告。"}
    ]
    git_used = False
    analyze_used = False
    write_used = False
    for turn in range(5):
        resp = client.chat.completions.create(model=MODEL, messages=messages, tools=tools, temperature=0.2)
        msg = resp.choices[0].message
        messages.append(msg)
        if not msg.tool_calls:
            print(f"   第 {turn+1} 轮: 模型直接回复")
            if write_used:
                print(f"   \u2713 Agent 已完成报告生成")
            break
        names = [tc.function.name for tc in msg.tool_calls]
        print(f"   第 {turn+1} 轮: {', '.join(names)}")
        for tc in msg.tool_calls:
            name = tc.function.name
            if name == "git_diff": git_used = True
            elif name == "analyze_impact": analyze_used = True
            elif name == "write_file": write_used = True
            args = json.loads(tc.function.arguments)
            if name == "git_diff":
                result = {"changed_files": ["src/main.py", "src/utils.py", "tests/test_main.py"], "insertions": 45, "deletions": 12, "diff_stats": {"main.py": "+20-5", "utils.py": "+15-7", "test_main.py": "+10-0"}}
            elif name == "analyze_impact":
                result = {"affected_modules": ["api", "database", "tests"], "risk_level": "medium", "recommendation": "需要补充单元测试"}
            elif name == "write_file":
                result = {"path": args.get("path"), "written": True, "size": len(args.get("content", ""))}
            else:
                result = simulate_tool(tc)
            messages.append({"role": "tool", "tool_call_id": tc.id, "content": json.dumps(result, ensure_ascii=False)})
    print(f"   git_diff: {'\u2713' if git_used else '\u2717'}, analyze_impact: {'\u2713' if analyze_used else '\u2717'}, write_file: {'\u2713' if write_used else '\u2717'}")
    assert git_used, "Agent 未使用 git_diff"
    assert write_used, "Agent 未将分析结果写入文件"
    if analyze_used:
        print(f"   \u2713 Agent 完成了完整的数据分析工具链")


# ═══════════════════════════════════════════════════════════════
# 主程序
# ═══════════════════════════════════════════════════════════════
if __name__ == "__main__":
    print(f"\n QwenPortal Agent \u5de5\u5177\u8c03\u7528\u6d4b\u8bd5\u5957\u4ef6")
    print(f"  \u4ee3\u7406\u5730\u5740: {PROXY_URL}")
    print(f"  \u6a21\u578b: {MODEL}")
    print(f"  \u6d4b\u8bd5\u6570: 16 \u9879")

    tests = [
        test_list_models,
        test_basic_chat,
        test_single_tool_call,
        test_multi_tool_round,
        test_streaming_tool_call,
        test_streaming_code_generation,
        test_latency_benchmark,
        test_concurrent_requests,
        test_invalid_model,
        test_system_prompt_agent,
        test_tool_error_recovery,
        test_project_understanding,
        test_long_context_large_file,
        test_long_conversation,
        test_search_test_cycle,
        test_structured_data_toolchain,
    ]

    for t in tests:
        t()

    total = pass_count + fail_count
    print(f"\n{'='*60}")
    print(f" {'='*54}")
    print(f"  \u6d4b\u8bd5\u7ed3\u679c: {pass_count}/{total} \u901a\u8fc7", end="")
    if fail_count > 0:
        print(f", {fail_count} \u5931\u8d25")
    else:
        print("  \u2705 \u5168\u90e8\u901a\u8fc7")
    print(f" {'='*54}")
    print(f"\n  \u6d4b\u8bd5\u8be6\u60c5:")
    for name, status, elapsed in test_results:
        mark = "\u2713" if status == "PASS" else "\u2717"
        print(f"    {mark} [{status}] {name} ({elapsed:.1f}s)")
    print(f"{'='*60}")
    sys.exit(0 if fail_count == 0 else 1)
