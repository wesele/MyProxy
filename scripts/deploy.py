#!/usr/bin/env python3
"""
QwenPortal 本地部署脚本
用法: python scripts/deploy.py [host] [user] [password]

从本机构建 Linux 二进制，通过 SSH 推送到远程测试机器并运行测试。
"""
import os, sys, time, json, subprocess, threading
from pathlib import Path

HOST = sys.argv[1] if len(sys.argv) > 1 else "192.168.31.233"
USER = sys.argv[2] if len(sys.argv) > 2 else "hao"
PASS = sys.argv[3] if len(sys.argv) > 3 else "934142"
PROJ = Path(__file__).resolve().parent.parent
REMOTE_DIR = f"/home/{USER}/qwenportal"

import paramiko

def run(cmd, cwd=None):
    print(f"  $ {cmd}")
    r = subprocess.run(cmd, shell=True, cwd=cwd or PROJ, capture_output=True, text=True)
    if r.returncode != 0:
        print(f"  ! FAILED: {r.stderr.strip()}")
    else:
        for l in r.stdout.strip().split("\n"):
            if l.strip():
                print(f"    {l}")
    return r.returncode

def ssh_exec(client, cmd, timeout=300):
    stdin, stdout, stderr = client.exec_command(cmd, timeout=timeout)
    try:
        exit_code = stdout.channel.recv_exit_status()
    except:
        exit_code = -1
    out = stdout.read().decode().strip()
    err = stderr.read().decode().strip()
    return exit_code, out, err

def scp(client, local_path, remote_path):
    sftp = client.open_sftp()
    sftp.put(str(local_path), remote_path)

def deploy():
    linux_bin = PROJ / "qwenportal_linux"

    # ── Step 1: Build ──────────────────────────────────────
    print("\n=== 1. 交叉编译 Linux 二进制 ===")
    env = os.environ.copy()
    env["GOOS"] = "linux"
    env["GOARCH"] = "amd64"
    r = subprocess.run(
        ["go", "build", "-o", str(linux_bin), "./cmd/qwenportal/"],
        cwd=PROJ, capture_output=True, text=True, env=env,
    )
    if r.returncode != 0:
        print(f"编译失败: {r.stderr}")
        return False
    print(f"   二进制: {linux_bin} ({os.path.getsize(linux_bin)//1024} KB)")

    # ── Step 2: SSH 连接 ───────────────────────────────────
    print(f"\n=== 2. 连接 {USER}@{HOST} ===")
    client = paramiko.SSHClient()
    client.set_missing_host_key_policy(paramiko.AutoAddPolicy())
    try:
        client.connect(HOST, username=USER, password=PASS, timeout=10)
        print("   连接成功")
    except Exception as e:
        print(f"   连接失败: {e}")
        return False

    try:
        # ── Step 3: 创建目录 ───────────────────────────────
        print(f"\n=== 3. 创建远程目录 {REMOTE_DIR} ===")
        ssh_exec(client, f"mkdir -p {REMOTE_DIR}/data")

        # ── Step 4: 上传文件 ───────────────────────────────
        print("\n=== 4. 上传文件 ===")
        sftp = client.open_sftp()

        # 上传前修改 config.yaml 确保 python3
        cfg_content = (PROJ / "config.yaml").read_text()
        cfg_content = cfg_content.replace("python: \"python\"", "python: \"python3\"")
        cfg_remote = f"{REMOTE_DIR}/config.yaml"
        with sftp.open(cfg_remote, "w") as f:
            f.write(cfg_content)
        print(f"   config.yaml ({len(cfg_content)} bytes)")

        # 上传 test_agent_tools.py
        remote_py = f"{REMOTE_DIR}/test_agent_tools.py"
        sftp.put(str(PROJ / "test_agent_tools.py"), remote_py)
        print(f"   test_agent_tools.py ({(PROJ / 'test_agent_tools.py').stat().st_size//1024} KB)")

        # 上传 linux 二进制（先删除旧文件避免磁盘冲突）
        import stat as statmod
        remote_bin = f"{REMOTE_DIR}/qwenportal_linux"
        ssh_exec(client, f"rm -f {remote_bin}", timeout=5)
        sftp.put(str(linux_bin), remote_bin)
        sftp.chmod(remote_bin, 0o755)
        print(f"   qwenportal_linux ({linux_bin.stat().st_size//1024} KB)")

        # 上传 webui 目录（递归上传所有文件，包括子目录）
        webui_local = PROJ / "webui"
        webui_remote = f"{REMOTE_DIR}/webui"
        for f in webui_local.rglob("*"):
            if f.is_file():
                rel = f.relative_to(webui_local).as_posix()
                remote_path = f"{webui_remote}/{rel}"
                parent = remote_path[:remote_path.rfind("/")]
                ssh_exec(client, f"mkdir -p {parent}", timeout=5)
                sftp.put(str(f), remote_path)
        sftp.close()

        # ── Step 5: 释放端口 8080 + 停止旧服务 ─────────────
        print("\n=== 5. 释放 8080 (stop docker + kill old) ===")
        ec, out, err = ssh_exec(client, """
            set -e
            docker stop file-server 2>/dev/null || true
            pkill -f qwenportal_linux 2>/dev/null || true
            sleep 2
            # 多重手段确保 8080 释放
            fuser -k 8080/tcp 2>/dev/null || true
            kill $(lsof -ti:8080 2>/dev/null) 2>/dev/null || true
            sleep 2
            # 等待端口真正释放，最多 30 秒
            for i in $(seq 1 30); do
                if ! ss -tlnp 2>/dev/null | grep -q ':8080 '; then
                    echo "port free after ${i}s"
                    break
                fi
                # 每 5 秒重试一次强杀
                if [ $((i % 5)) -eq 0 ]; then
                    fuser -k 8080/tcp 2>/dev/null || true
                    kill $(lsof -ti:8080 2>/dev/null) 2>/dev/null || true
                fi
                sleep 1
            done
            cd /home/hao/qwenportal
            rm -f data/qwenportal.db data/admin_key.txt
        """.strip(), timeout=60)
        print(f"   {out.strip()[:200]}")
        rest = out.strip()

        # ── Step 6: 启动新服务 ─────────────────────────────
        print("\n=== 6. 启动新服务 ===")
        # 先清空日志
        ssh_exec(client, f"rm -f /tmp/qwenportal.log", timeout=5)
        transport = client.get_transport()
        chan = transport.open_session()
        chan.exec_command(f"cd {REMOTE_DIR} && nohup ./qwenportal_linux > /tmp/qwenportal.log 2>&1 & disown")
        chan.close()
        # 等待进程启动（最多 10 秒）
        started = False
        for i in range(10):
            time.sleep(1)
            ec, out, err = ssh_exec(client, "pgrep -a qwenportal_linux 2>/dev/null || echo 'not found'", timeout=5)
            if 'qwenportal_linux' in out:
                print(f"   服务已启动 (第 {i+1} 秒)")
                started = True
                break
        if not started:
            ec, out, err = ssh_exec(client, "tail -20 /tmp/qwenportal.log", timeout=5)
            print(f"   ! 启动失败: {out[:300].strip()}")

        # ── Step 7: 等待服务就绪 + 添加 Provider ────────────
        print("\n=== 7. 等待服务就绪 + 添加 Provider ===")
        ready = False
        for i in range(30):
            ec, out, err = ssh_exec(client, "curl -sf http://127.0.0.1:8080/v1/models 2>&1", timeout=5)
            if ec == 0 and out and '"object"' in out:
                ready = True
                print(f"   服务就绪 (第 {i+1} 次尝试)")
                break
            # 每 5 次检查一次日志看看是否启动失败
            if i > 0 and i % 5 == 0:
                ec2, out2, err2 = ssh_exec(client, "pgrep -a qwenportal_linux 2>/dev/null || echo 'not found'", timeout=5)
                if 'not found' in out2:
                    ec2, out2, err2 = ssh_exec(client, "tail -5 /tmp/qwenportal.log", timeout=5)
                    if 'address already in use' in out2:
                        print("   ! 端口 8080 仍被占用，尝试强杀...")
                        ssh_exec(client, "fuser -k 8080/tcp 2>/dev/null; kill $(lsof -ti:8080 2>/dev/null) 2>/dev/null; sleep 2", timeout=10)
                        # 重新启动
                        transport = client.get_transport()
                        chan = transport.open_session()
                        chan.exec_command(f"cd {REMOTE_DIR} && nohup ./qwenportal_linux > /tmp/qwenportal.log 2>&1 & disown")
                        chan.close()
                        time.sleep(3)
                    else:
                        print(f"   ! 进程已退出: {out2[:200].strip()}")
                        # 重新启动
                        transport = client.get_transport()
                        chan = transport.open_session()
                        chan.exec_command(f"cd {REMOTE_DIR} && nohup ./qwenportal_linux > /tmp/qwenportal.log 2>&1 & disown")
                        chan.close()
                        time.sleep(3)
            time.sleep(2)
        if not ready:
            ec, out, err = ssh_exec(client, "tail -20 /tmp/qwenportal.log 2>/dev/null", timeout=5)
            print(f"   ! 服务未就绪. 日志: {out[:500].strip()}")

        api_key = "eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.eyJpZCI6ImVmODA4ZjA2LTE2NTgtNDRlOS1hMTYzLTNlZWJhOTlhZTkxYiIsImxhc3RfcGFzc3dvcmRfY2hhbmdlIjoxNzcxMjY4NTQxLCJleHAiOjE3ODE2NDEyMzF9.XrnkCW7_x1A_KfUMN65VkJZRr3XeChxzxRgoMelXiRI"
        provider_name = "QwenTest"

        # Delete existing provider with the same name to avoid duplicates
        ec, out, err = ssh_exec(client, f"curl -sf http://127.0.0.1:8080/admin/api/providers 2>/dev/null || echo '[]'", timeout=5)
        if ec == 0 and out:
            import json as _json
            try:
                existing = _json.loads(out)
                for p in existing:
                    if p.get("name") == provider_name:
                        pid = p.get("id")
                        ssh_exec(client, f"curl -sf -X DELETE http://127.0.0.1:8080/admin/api/providers/{pid} 2>/dev/null", timeout=5)
                        print(f"   Removed existing provider '{provider_name}' (id={pid})")
            except:
                pass

        provider = {
            "name": provider_name, "provider_type": "openai",
            "base_url": "https://qwen.aikit.club/v1", "api_key": api_key,
            "models": ["qwen3.6-plus"], "is_active": True,
        }
        provider_json = json.dumps(provider)
        sftp = client.open_sftp()
        with sftp.open("/tmp/provider.json", "w") as f:
            f.write(provider_json)
        sftp.close()
        ec, out, err = ssh_exec(client, "curl -sf -X POST http://127.0.0.1:8080/admin/api/providers -H 'Content-Type: application/json' -d @/tmp/provider.json")
        if ec == 0:
            print("   Provider 添加成功")
        else:
            print(f"   ! 添加 Provider 失败 (exit={ec}): {(err or out)[:200]}")
            ec2, o2, e2 = ssh_exec(client, "cat /home/hao/qwenportal/data/admin_key.txt", timeout=5)
            if o2: print(f"   Admin key: {o2.strip()}")

        # ── Step 8: 运行测试 ──────────────────────────────
        print("\n=== 8. 安装依赖 + 运行测试套件 ===")
        ec, out, err = ssh_exec(client, "pip3 install openai flask paramiko -q 2>/dev/null; echo 'deps ok'", timeout=60)
        cmd = f"cd {REMOTE_DIR} && PROXY_URL=http://127.0.0.1:8080/v1 MODEL=qwen3.6-plus python3 test_agent_tools.py"
        stdin, stdout, stderr = client.exec_command(cmd, timeout=600)

        def print_stream(stream, prefix=""):
            for line in iter(stream.readline, ""):
                print(f"{prefix}{line}", end="")
                sys.stdout.flush()

        out_thread = threading.Thread(target=print_stream, args=(stdout, ""))
        err_thread = threading.Thread(target=print_stream, args=(stderr, "  [stderr] "))
        out_thread.start()
        err_thread.start()
        out_thread.join()
        err_thread.join()

        exit_code = stdout.channel.recv_exit_status()
        print(f"\n   测试退出码: {exit_code}")

        # ── Step 9: 恢复 Docker 容器 ──────────────────────
        print("\n=== 9. 恢复 Docker 容器 ===")
        ec, out, err = ssh_exec(client, "docker start file-server 2>&1 && echo 'restarted' || echo 'not restarted'")
        print(f"   {'restarted' if 'restarted' in out else out.strip()}")

        return exit_code == 0

    finally:
        client.close()

if __name__ == "__main__":
    print(f"{'='*60}")
    print(f" QwenPortal 部署脚本")
    print(f"   目标: {USER}@{HOST}")
    print(f"{'='*60}")
    success = deploy()
    print(f"\n{'='*60}")
    if success:
        print(" ✅ 部署 + 测试全部通过")
    else:
        print(" ❌ 部署或测试失败")
    print(f"{'='*60}")
    sys.exit(0 if success else 1)
