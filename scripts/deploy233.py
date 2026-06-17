#!/usr/bin/env python3
"""
QwenPortal one-click deploy to 233
Usage: python scripts/deploy233.py [--skip-test] [--skip-build] [--skip-provider]
"""
import os, sys, time, json, subprocess, paramiko

# Fix Windows GBK console encoding
if sys.platform == "win32":
    os.environ["PYTHONIOENCODING"] = "utf-8"
    try:
        sys.stdout.reconfigure(encoding="utf-8", errors="replace")
        sys.stderr.reconfigure(encoding="utf-8", errors="replace")
    except Exception:
        pass

# ── 配置 ──────────────────────────────────────────────────
HOST = "192.168.31.233"
USER = "hao"
PASS = "934142"
PROJ = os.path.dirname(os.path.dirname(os.path.abspath(__file__)))
REMOTE_DIR = f"/home/{USER}/qwenportal"
PROVIDER_NAME = "QwenTest"
PROVIDER_URL = "https://qwen.aikit.club/v1"
PROVIDER_KEY = "eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.eyJpZCI6ImVmODA4ZjA2LTE2NTgtNDRlOS1hMTYzLTNlZWJhOTlhZTkxYiIsImxhc3RfcGFzc3dvcmRfY2hhbmdlIjoxNzcxMjY4NTQxLCJleHAiOjE3ODE2NDEyMzF9.XrnkCW7_x1A_KfUMN65VkJZRr3XeChxzxRgoMelXiRI"
PROVIDER_MODELS = ["qwen3.6-plus"]
TEST_MODEL = "qwen3.6-plus"
SKIP_TEST = "--skip-test" in sys.argv
SKIP_BUILD = "--skip-build" in sys.argv
SKIP_PROVIDER = "--skip-provider" in sys.argv


def msg(text):
    print(f"  {text}")


def ssh_connect():
    client = paramiko.SSHClient()
    client.set_missing_host_key_policy(paramiko.AutoAddPolicy())
    client.connect(HOST, username=USER, password=PASS, timeout=10)
    return client


def ssh_run(client, cmd, timeout=30):
    stdin, stdout, stderr = client.exec_command(cmd, timeout=timeout)
    exit_code = stdout.channel.recv_exit_status()
    out = stdout.read().decode(errors="replace").strip()
    err = stderr.read().decode(errors="replace").strip()
    return exit_code, out, err


def ssh_run_no_timeout(client, cmd):
    transport = client.get_transport()
    chan = transport.open_session()
    chan.exec_command(cmd)
    time.sleep(0.5)
    chan.close()


def scp_upload(client, local_path, remote_path):
    sftp = client.open_sftp()
    try:
        sftp.put(str(local_path), remote_path)
    finally:
        sftp.close()


def kill_port_8080(client):
    """多重手段确保端口 8080 释放，最多等待 40 秒"""
    for attempt in range(8):
        # 先杀进程
        ssh_run(client, "pkill -9 -f qwenportal_linux 2>/dev/null", timeout=5)
        ssh_run(client, "docker stop file-server 2>/dev/null", timeout=5)
        ssh_run(client, "kill -9 $(lsof -ti:8080 2>/dev/null) 2>/dev/null", timeout=5)
        ssh_run(client, "fuser -k -9 8080/tcp 2>/dev/null", timeout=5)
        # 检查端口
        for i in range(5):
            ec, out, _ = ssh_run(client, "ss -tlnp | grep :8080 ", timeout=5)
            if not out.strip():
                msg(f"端口 8080 已释放 (第 {attempt*5+i+1} 秒)")
                return True
            time.sleep(1)
    msg("WARNING: 端口 8080 仍被占用，尝试继续...")
    return False


def build_linux_binary():
    print("\n[1/8] 交叉编译 Linux 二进制...")
    linux_bin = os.path.join(PROJ, "qwenportal_linux")
    env = os.environ.copy()
    env["GOOS"] = "linux"
    env["GOARCH"] = "amd64"
    r = subprocess.run(
        ["go", "build", "-o", linux_bin, "./cmd/qwenportal/"],
        cwd=PROJ, capture_output=True, text=True, env=env,
    )
    if r.returncode != 0:
        msg(f"FAIL: {r.stderr.strip()}")
        return False
    size_kb = os.path.getsize(linux_bin) // 1024
    msg(f"OK: qwenportal_linux ({size_kb} KB)")
    return True


def upload_files(client):
    print("\n[2/8] 上传文件...")
    linux_bin = os.path.join(PROJ, "qwenportal_linux")
    sftp = client.open_sftp()

    # 创建目录
    ssh_run(client, f"mkdir -p {REMOTE_DIR}/data", timeout=5)

    # 上传 config.yaml (python -> python3)
    cfg = open(os.path.join(PROJ, "config.yaml"), "r").read()
    cfg = cfg.replace('python: "python"', 'python: "python3"')
    remote_cfg = f"{REMOTE_DIR}/config.yaml"
    with sftp.open(remote_cfg, "w") as f:
        f.write(cfg)
    msg(f"config.yaml ({len(cfg)} bytes)")

    # 上传 test_agent_tools.py
    test_file = os.path.join(PROJ, "test_agent_tools.py")
    sftp.put(test_file, f"{REMOTE_DIR}/test_agent_tools.py")
    msg(f"test_agent_tools.py ({os.path.getsize(test_file)//1024} KB)")

    # 上传二进制
    remote_bin = f"{REMOTE_DIR}/qwenportal_linux"
    ssh_run(client, f"rm -f {remote_bin}", timeout=5)
    sftp.put(linux_bin, remote_bin)
    sftp.chmod(remote_bin, 0o755)
    msg(f"qwenportal_linux ({os.path.getsize(linux_bin)//1024} KB)")

    # 上传 webui 目录
    webui_local = os.path.join(PROJ, "webui")
    count = 0
    for root, dirs, files in os.walk(webui_local):
        for fname in files:
            local_f = os.path.join(root, fname)
            rel = os.path.relpath(local_f, webui_local).replace("\\", "/")
            remote_path = f"{REMOTE_DIR}/webui/{rel}"
            parent = remote_path[:remote_path.rfind("/")]
            ssh_run(client, f"mkdir -p {parent}", timeout=5)
            sftp.put(local_f, remote_path)
            count += 1
    sftp.close()
    msg(f"webui/ ({count} 个文件)")


def kill_old_service(client):
    print("\n[3/8] 停止旧服务 + 清理孤儿 Flask...")
    kill_port_8080(client)
    # 杀死之前部署残留的孤儿 Flask 进程
    ssh_run(client, "pkill -f 'python3 app.py' 2>/dev/null; pkill -f 'python app.py' 2>/dev/null", timeout=5)
    ssh_run(client, "sleep 2", timeout=5)


def start_service(client):
    print("\n[4/8] 启动新服务...")
    ssh_run(client, "rm -f /tmp/qwenportal.log", timeout=5)

    ssh_run_no_timeout(client,
        f"cd {REMOTE_DIR} && nohup ./qwenportal_linux > /tmp/qwenportal.log 2>&1 & disown")

    # 等待进程出现
    for i in range(15):
        time.sleep(1)
        ec, out, _ = ssh_run(client, "pgrep -f qwenportal_linux", timeout=5)
        if out.strip():
            msg(f"进程已启动 (PID: {out.strip().split()[0]}, {i+1}s)")
            return True
    msg("WARNING: 进程启动检测超时")
    return False


def wait_for_service(client):
    print("\n[5/8] 等待服务就绪...")
    for i in range(30):
        ec, out, _ = ssh_run(client, "curl -sf http://127.0.0.1:8080/v1/models 2>&1", timeout=5)
        if ec == 0 and '"object"' in out:
            msg(f"服务就绪 (第 {i+1} 次尝试)")
            return True
        # 每 10 秒检查一次进程是否存活
        if i > 0 and i % 10 == 0:
            ec2, out2, _ = ssh_run(client, "pgrep -f qwenportal_linux 2>/dev/null || echo DEAD", timeout=5)
            if "DEAD" in out2:
                msg("进程已退出，重新启动...")
                ssh_run_no_timeout(client,
                    f"cd {REMOTE_DIR} && nohup ./qwenportal_linux > /tmp/qwenportal.log 2>&1 & disown")
                time.sleep(3)
        time.sleep(2)
    # 输出日志帮助排查
    _, log_out, _ = ssh_run(client, "tail -20 /tmp/qwenportal.log", timeout=5)
    msg(f"WARNING: 服务未就绪\n    日志: {log_out[:500]}")
    return False


def setup_provider(client):
    print("\n[6/8] 配置 Provider...")
    # 获取 admin key
    ec, admin_key, _ = ssh_run(client, f"cat {REMOTE_DIR}/data/admin_key.txt 2>/dev/null", timeout=5)
    if admin_key:
        msg(f"Admin key: {admin_key[:20]}...")

    # 删除同名旧 provider
    ec, out, _ = ssh_run(client, "curl -sf http://127.0.0.1:8080/admin/api/providers 2>/dev/null || echo []", timeout=10)
    try:
        providers = json.loads(out)
        for p in providers:
            if p.get("name") == PROVIDER_NAME:
                pid = p.get("id")
                ssh_run(client, f"curl -sf -X DELETE http://127.0.0.1:8080/admin/api/providers/{pid}", timeout=5)
                msg(f"删除旧 provider id={pid}")
    except (json.JSONDecodeError, TypeError):
        pass

    # 创建新 provider
    provider = {
        "name": PROVIDER_NAME, "provider_type": "openai",
        "base_url": PROVIDER_URL, "api_key": PROVIDER_KEY,
        "models": PROVIDER_MODELS, "is_active": True,
    }
    provider_json = json.dumps(provider)

    # 通过 SFTP 上传 JSON 再 curl
    sftp = client.open_sftp()
    with sftp.open("/tmp/provider.json", "w") as f:
        f.write(provider_json)
    sftp.close()

    ec, out, err = ssh_run(client,
        "curl -sf -X POST http://127.0.0.1:8080/admin/api/providers "
        "-H 'Content-Type: application/json' -d @/tmp/provider.json", timeout=10)
    if ec == 0 and out:
        msg(f"Provider 创建成功")
        return True
    else:
        msg(f"WARNING: Provider 创建返回 (exit={ec}): {err or out}")
        return False


def run_tests(client):
    print("\n[7/8] 运行测试套件...")
    # 确保 openai 已安装
    ssh_run(client, "pip3 install openai flask -q 2>/dev/null", timeout=60)

    test_cmd = (
        f"cd {REMOTE_DIR} && "
        f"PROXY_URL=http://127.0.0.1:8080/v1 "
        f"MODEL={TEST_MODEL} "
        f"PYTHONIOENCODING=utf-8 "
        f"python3 test_agent_tools.py"
    )

    transport = client.get_transport()
    chan = transport.open_session()
    chan.exec_command(test_cmd)

    output_lines = []
    start = time.time()
    last_print = time.time()

    while time.time() - start < 600:
        if chan.recv_ready():
            data = chan.recv(8192).decode(errors="replace")
            output_lines.append(data)
            # 每 10 秒打印一次进度
            if time.time() - last_print > 10:
                sys.stdout.write(".")
                sys.stdout.flush()
                last_print = time.time()
        if chan.exit_status_ready():
            # 读取剩余输出
            while chan.recv_ready():
                data = chan.recv(8192).decode(errors="replace")
                output_lines.append(data)
            break
        time.sleep(0.5)

    exit_code = chan.recv_exit_status()
    chan.close()

    # 输出测试结果（过滤 GBK 无法编码的字符）
    full_output = "".join(output_lines)
    print()
    for line in full_output.split("\n"):
        try:
            print(f"  {line}")
        except UnicodeEncodeError:
            # 过滤掉无法编码的字符
            safe = line.encode("gbk", errors="replace").decode("gbk")
            print(f"  {safe}")

    # 提取最终结果
    for line in reversed(full_output.split("\n")):
        if "测试结果" in line or "EXIT_CODE" in line or "全部通过" in line:
            msg(f"最终: {line.strip()}")
            break

    return exit_code == 0


def restore_docker(client):
    print("\n[8/8] 恢复 Docker...")
    ssh_run(client, "docker start file-server 2>/dev/null && echo restarted || echo skipped", timeout=10)


def main():
    print("=" * 60)
    print("  QwenPortal 一键部署到 233")
    print(f"  目标: {USER}@{HOST}")
    print("=" * 60)

    # 1. 编译
    if SKIP_BUILD:
        print("\n[1/8] 跳过编译 (--skip-build)")
    else:
        if not build_linux_binary():
            print("\n[FAIL] 编译失败")
            return False, False

    # 2. 连接
    print("\n[连接] SSH -> " + HOST)
    try:
        client = ssh_connect()
        msg("连接成功")
    except Exception as e:
        msg(f"连接失败: {e}")
        return False, False

    deploy_ok, test_ok = False, True
    try:
        # 3. 上传文件
        upload_files(client)

        # 4. 停止旧服务
        kill_old_service(client)

        # 5. 启动新服务
        start_service(client)

        # 6. 等待就绪
        if not wait_for_service(client):
            msg("服务启动超时，尝试继续...")

        # 7. 配置 Provider（可跳过）
        if SKIP_PROVIDER:
            print("\n[6/8] 跳过 Provider 配置 (--skip-provider)")
        else:
            setup_provider(client)

        # 8. 验证 API 可用
        print("\n[7/8] 验证 API 可用性...")
        ec, out, _ = ssh_run(client, "curl -sf http://127.0.0.1:8080/v1/models", timeout=10)
        api_ok = ec == 0 and '"object"' in out
        if api_ok:
            msg("API /v1/models 响应正常")
        else:
            msg("WARNING: API 未响应")

        # 9. 运行测试（结果不影响部署成功判定）
        if SKIP_TEST:
            print("\n[8/8] 跳过测试 (--skip-test)")
        else:
            test_ok = run_tests(client)

        # 10. 恢复 Docker
        restore_docker(client)

        # 部署成功 = 服务启动 + API 可用
        deploy_ok = api_ok

    except Exception as e:
        msg(f"异常: {e}")
        deploy_ok = False
    finally:
        client.close()

    return deploy_ok, test_ok


if __name__ == "__main__":
    print()
    result = main()
    if isinstance(result, tuple):
        deploy_ok, test_ok = result
    else:
        deploy_ok, test_ok = result, result
    print()
    print("=" * 60)
    if deploy_ok:
        print("  DEPLOY OK - 192.168.31.233:8080")
        if not SKIP_TEST:
            if test_ok:
                print("  TESTS OK - all passed")
            else:
                print("  TESTS WARN - some tests failed (not a deploy issue)")
    else:
        print("  DEPLOY FAILED")
    print("=" * 60)
    sys.exit(0 if deploy_ok else 1)
