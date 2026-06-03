# Release Notes

## 部署信息

| 项目 | 值 |
|------|-----|
| 测试机器 | 192.168.31.233 |
| SSH 用户 | hao |
| 端口 | 8080 |
| 部署目录 | `/home/hao/qwenportal/` |
| 日志文件 | `/tmp/qwenportal.log` |

## 运行中服务

| 服务 | 端口 | 说明 |
|------|------|------|
| QwenPortal | 8080 | Go API 网关 + Flask Web UI |
| file-server (Docker) | 8080 (部署时暂停) | 文件管理，部署恢复后自动重启 |

## 部署方式

```bash
# 从本机一键部署到测试机
python scripts/deploy.py 192.168.31.233 hao 934142
```

部署流程：
1. 交叉编译 Linux amd64 二进制
2. SFTP 上传二进制 + 配置 + Web UI + 测试脚本
3. 暂停 Docker file-server 释放 8080 端口
4. 启动 QwenPortal
5. 自动添加测试 Provider (qwen3.6-plus)
6. 运行 16 项 Agent 工具调用测试
7. 恢复 Docker file-server

## 测试结果 (v0.1)

```
测试结果: 16/16 通过  ✅ 全部通过

  ✓ [PASS] 基础连通性 - 列出模型            (0.2s)
  ✓ [PASS] 基础对话 - 代码生成              (6.0s)
  ✓ [PASS] 单轮工具调用                     (3.9s)
  ✓ [PASS] 多轮工具调用                     (7.6s)
  ✓ [PASS] 流式工具调用                     (5.2s)
  ✓ [PASS] 流式代码生成 SSE                 (18.6s)
  ✓ [PASS] 性能基准 平均 3.16s              (18.7s)
  ✓ [PASS] 并发 3 请求                      (17.1s)
  ✓ [PASS] 错误处理                         (0.0s)
  ✓ [PASS] 系统提示词 Agent 角色            (52.4s)
  ✓ [PASS] 工具错误恢复                     (15.9s)
  ✓ [PASS] 多文件项目分析 (9 文件)          (34.3s)
  ✓ [PASS] 大文件上下文上限 (21k 字符)      (9.5s)
  ✓ [PASS] 长对话积累 (12 轮)               (29.1s)
  ✓ [PASS] 搜索+测试循环                    (25.1s)
  ✓ [PASS] 结构化数据工具链                 (20.3s)
```

## API 端点

| 端点 | 方法 | 说明 |
|------|------|------|
| `http://192.168.31.233:8080/` | GET | Web UI (重定向到 /admin/dashboard) |
| `http://192.168.31.233:8080/v1/models` | GET | 模型列表 |
| `http://192.168.31.233:8080/v1/chat/completions` | POST | 对话补全 |
| `http://192.168.31.233:8080/v1/embeddings` | POST | 向量嵌入 |
| `http://192.168.31.233:8080/v1/messages` | POST | Claude 兼容接口 |
| `http://192.168.31.233:8080/admin/dashboard` | GET | Web 仪表盘 |
| `http://192.168.31.233:8080/admin/providers` | GET | Provider 管理 |
| `http://192.168.31.233:8080/admin/keys` | GET | API Key 管理 |

## 配置模型

当前配置的 Provider:

- **名称**: QwenTest
- **类型**: OpenAI 兼容
- **Base URL**: https://qwen.aikit.club/v1
- **模型**: qwen3.6-plus

## 注意事项

- 测试机器的 Docker `file-server` 容器长期占用 8080 端口，部署时会自动暂停，测试完成后恢复
- 管理 API 从 localhost 访问无需认证，远程访问需 `Authorization: Bearer <admin-key>`
- Flask Web UI 使用随机端口 (5100+)，QwenPortal 自动反向代理
