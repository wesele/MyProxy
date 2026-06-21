#!/usr/bin/env python3
"""
QwenPortal one-click deploy to Tencent Cloud
Usage: python scripts/deployTencent.py [--skip-build] [--skip-provider] [--skip-test]
"""
import os, sys, time, json, subprocess, paramiko

if sys.platform == "win32":
    os.environ["PYTHONIOENCODING"] = "utf-8"
    try:
        sys.stdout.reconfigure(encoding="utf-8", errors="replace")
        sys.stderr.reconfigure(encoding="utf-8", errors="replace")
    except Exception:
        pass

HOST = "49.235.121.91"
USER = "ubuntu"
KEY_FILE = "c:\\wh\\ssh.pem"
PROJ = os.path.dirname(os.path.dirname(os.path.abspath(__file__)))
REMOTE_DIR = f"/home/{USER}/qwenportal"
CF_DOWNLOAD_URL = "https://my-proxy.pages.dev/download"
PROVIDER_NAME = "QwenTest"
PROVIDER_URL = "https://qwen.aikit.club/v1"
PROVIDER_KEY = "eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.eyJpZCI6ImVmODA4ZjA2LTE2NTgtNDRlOS1hMTYzLTNlZWJhOTlhZTkxYiIsImxhc3RfcGFzc3dvcmRfY2hhbmdlIjoxNzcxMjY4NTQxLCJleHAiOjE3ODE2NDEyMzF9.XrnkCW7_x1A_KfUMN65VkJZRr3XeChxzxRgoMelXiRI"
PROVIDER_MODELS = ["qwen3.6-plus"]
TEST_MODEL = "qwen3.6-plus"
SKIP_BUILD = "--skip-build" in sys.argv
SKIP_PROVIDER = "--skip-provider" in sys.argv
SKIP_TEST = "--skip-test" in sys.argv


def msg(text):
    print(f"  {text}")


def ssh_connect():
    client = paramiko.SSHClient()
    client.set_missing_host_key_policy(paramiko.AutoAddPolicy())
    key = paramiko.RSAKey.from_private_key_file(KEY_FILE)
    client.connect(HOST, username=USER, pkey=key, timeout=15, banner_timeout=30, auth_timeout=30)
    client.get_transport().set_keepalive(30)
    return client


def ssh_run(client, cmd, timeout=30):
    stdin, stdout, stderr = client.exec_command(cmd, timeout=timeout)
    ec = stdout.channel.recv_exit_status()
    return ec, stdout.read().decode(errors="replace").strip(), stderr.read().decode(errors="replace").strip()


def ssh_run_bg(client, cmd):
    transport = client.get_transport()
    chan = transport.open_session()
    chan.exec_command(cmd)
    time.sleep(0.5)
    chan.close()


def kill_port_8080(client):
    for attempt in range(8):
        ssh_run(client, "pkill -9 -f qwenportal_linux 2>/dev/null", timeout=5)
        ssh_run(client, "kill -9 $(lsof -ti:8080 2>/dev/null) 2>/dev/null", timeout=5)
        ssh_run(client, "fuser -k -9 8080/tcp 2>/dev/null", timeout=5)
        for i in range(5):
            ec, out, _ = ssh_run(client, "ss -tlnp | grep :8080 ", timeout=5)
            if not out.strip():
                msg(f"端口 8080 已释放 (第 {attempt*5+i+1} 秒)")
                return True
            time.sleep(1)
    return False


def build_linux_binary():
    print("\n[1/6] 交叉编译 Linux 二进制...")
    linux_bin = os.path.join(PROJ, "qwenportal_linux")
    env = os.environ.copy()
    env["GOOS"] = "linux"
    env["GOARCH"] = "amd64"
    r = subprocess.run(["go", "build", "-o", linux_bin, "./cmd/qwenportal/"], cwd=PROJ, capture_output=True, text=True, env=env)
    if r.returncode != 0:
        msg(f"FAIL: {r.stderr.strip()}")
        return False
    msg(f"OK: qwenportal_linux ({os.path.getsize(linux_bin)//1024} KB)")
    return True


def deploy():
    print("=" * 60)
    print("  QwenPortal 一键部署到腾讯云")
    print(f"  目标: {USER}@{HOST}")
    print("=" * 60)

    if not SKIP_BUILD:
        if not build_linux_binary():
            return False

    print("\n[连接] SSH -> " + HOST)
    try:
        client = ssh_connect()
        msg("连接成功")
    except Exception as e:
        msg(f"连接失败: {e}")
        return False

    deploy_ok = False
    try:
        print("\n[1/6] 上传配置文件 + WebUI...")
        ssh_run(client, f"mkdir -p {REMOTE_DIR}/data", timeout=5)
        ssh_run(client, f"mkdir -p {REMOTE_DIR}/webui", timeout=5)

        sftp = client.open_sftp()
        cfg = open(os.path.join(PROJ, "config.yaml"), "r").read().replace('python: "python"', 'python: "python3"')
        with sftp.open(f"{REMOTE_DIR}/config.yaml", "w") as f:
            f.write(cfg)
        sftp.close()
        msg(f"config.yaml ({len(cfg)} bytes)")

        test_file = os.path.join(PROJ, "test_agent_tools.py")
        sftp = client.open_sftp()
        sftp.put(test_file, f"{REMOTE_DIR}/test_agent_tools.py")
        sftp.close()
        msg(f"test_agent_tools.py ({os.path.getsize(test_file)//1024} KB)")

        webui_local = os.path.join(PROJ, "webui")
        count = 0
        for root, dirs, files in os.walk(webui_local):
            for fname in files:
                local_f = os.path.join(root, fname)
                rel = os.path.relpath(local_f, webui_local).replace("\\", "/")
                remote_path = f"{REMOTE_DIR}/webui/{rel}"
                parent = remote_path[:remote_path.rfind("/")]
                ssh_run(client, f"mkdir -p {parent}", timeout=5)
                sftp = client.open_sftp()
                sftp.put(local_f, remote_path)
                sftp.close()
                count += 1
        msg(f"webui/ ({count} 个文件)")

        print("\n[2/6] 从 Cloudflare 下载二进制...")
        ec, out, err = ssh_run(client, f"curl -fL --connect-timeout 10 --max-time 600 '{CF_DOWNLOAD_URL}' -o {REMOTE_DIR}/qwenportal_linux && chmod 755 {REMOTE_DIR}/qwenportal_linux", timeout=620)
        if ec != 0:
            raise RuntimeError(f"Cloudflare 下载失败: {err[:200]}")
        size_kb = 0
        ec2, out2, _ = ssh_run(client, f"stat --format=%s {REMOTE_DIR}/qwenportal_linux 2>/dev/null || echo 0", timeout=5)
        if out2 and out2 != "0":
            size_kb = int(out2) // 1024
        msg(f"qwenportal_linux ({size_kb} KB) 下载完成")

        print("\n[3/6] 停止旧服务...")
        kill_port_8080(client)
        ssh_run(client, "pkill -f 'python3 app.py' 2>/dev/null; pkill -f 'python app.py' 2>/dev/null", timeout=5)
        ssh_run(client, "sleep 2", timeout=5)

        print("\n[4/6] 启动新服务...")
        ssh_run(client, "rm -f /tmp/qwenportal.log", timeout=5)
        ssh_run_bg(client, f"cd {REMOTE_DIR} && nohup ./qwenportal_linux > /tmp/qwenportal.log 2>&1 & disown")
        started = False
        for i in range(15):
            time.sleep(1)
            ec, out, _ = ssh_run(client, "pgrep -f qwenportal_linux", timeout=5)
            if out.strip():
                msg(f"进程已启动 (PID: {out.strip().split()[0]}, {i+1}s)")
                started = True
                break
        if not started:
            msg("WARNING: 进程启动检测超时")

        print("\n[5/6] 等待服务就绪...")
        ready = False
        for i in range(30):
            ec, out, _ = ssh_run(client, "curl -sf http://127.0.0.1:8080/v1/models 2>&1", timeout=5)
            if ec == 0 and '"object"' in out:
                msg(f"服务就绪 (第 {i+1} 次尝试)")
                ready = True
                break
            if i > 0 and i % 10 == 0:
                ec2, out2, _ = ssh_run(client, "pgrep -f qwenportal_linux 2>/dev/null || echo DEAD", timeout=5)
                if "DEAD" in out2:
                    msg("进程已退出，重新启动...")
                    ssh_run_bg(client, f"cd {REMOTE_DIR} && nohup ./qwenportal_linux > /tmp/qwenportal.log 2>&1 & disown")
                    time.sleep(3)
            time.sleep(2)
        if not ready:
            _, log_out, _ = ssh_run(client, "tail -20 /tmp/qwenportal.log", timeout=5)
            msg(f"WARNING: 服务未就绪\n    日志: {log_out[:500]}")

        if not SKIP_PROVIDER:
            print("\n[6/6] 配置 Provider...")
            ec, admin_key, _ = ssh_run(client, f"cat {REMOTE_DIR}/data/admin_key.txt 2>/dev/null", timeout=5)
            if admin_key:
                msg(f"Admin key: {admin_key[:20]}...")
            ec, out, _ = ssh_run(client, "curl -sf http://127.0.0.1:8080/admin/api/providers 2>/dev/null || echo []", timeout=10)
            try:
                providers = json.loads(out)
                for p in providers:
                    if p.get("name") == PROVIDER_NAME:
                        pid = p.get("id")
                        ssh_run(client, f"curl -sf -X DELETE http://127.0.0.1:8080/admin/api/providers/{pid}", timeout=5)
            except Exception:
                pass
            provider = {"name": PROVIDER_NAME, "provider_type": "openai", "base_url": PROVIDER_URL, "api_key": PROVIDER_KEY, "models": PROVIDER_MODELS, "is_active": True}
            sftp = client.open_sftp()
            with sftp.open("/tmp/provider.json", "w") as f:
                f.write(json.dumps(provider))
            sftp.close()
            ec, out, err = ssh_run(client, "curl -sf -X POST http://127.0.0.1:8080/admin/api/providers -H 'Content-Type: application/json' -d @/tmp/provider.json", timeout=10)
            if ec == 0 and out:
                msg("Provider 创建成功")
            else:
                msg(f"WARNING: Provider 创建失败 (exit={ec}): {err[:100]}")

        # Verify API
        ec, out, _ = ssh_run(client, "curl -sf http://127.0.0.1:8080/v1/models", timeout=10)
        deploy_ok = ec == 0 and '"object"' in out

    except Exception as e:
        msg(f"异常: {e}")
    finally:
        client.close()

    return deploy_ok


if __name__ == "__main__":
    print()
    ok = deploy()
    print()
    print("=" * 60)
    if ok:
        print("  DEPLOY OK - 49.235.121.91:8080")
    else:
        print("  DEPLOY FAILED")
    print("=" * 60)
    sys.exit(0 if ok else 1)
