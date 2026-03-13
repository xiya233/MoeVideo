# MoeVideo `yt-dlp` 自定义参数配置指南

本文档用于说明后台 **Site Settings -> Import / yt-dlp** 的配置方式、参数生效范围、常见错误和排查方法。  
适用范围：**URL 导入**（`POST /api/v1/imports/url/start`），不影响 BT 种子导入。

## 1. 功能概览

MoeVideo 支持为 URL 导入配置两种 `yt-dlp` 参数模式：

- `safe`（白名单安全模式）：通过结构化字段配置常用参数。
- `advanced`（高级原始参数模式）：直接填写两段参数字符串（metadata/download 分开）。

配置入口统一在后台：

- 页面：`/admin/site-settings`
- 模块：`Import / yt-dlp`

## 2. 生效机制（重点）

配置不是“实时作用于所有任务”，而是：

- 在创建 URL 导入任务时（`POST /api/v1/imports/url/start`）读取当时的站点配置。
- 将参数快照写入 `video_import_jobs`。
- Worker 后续严格按该任务快照执行。

这意味着：

- 新建任务会使用最新配置。
- 已排队/运行中的旧任务不会被后续配置修改影响。

## 3. 配置入口与字段说明

管理端读取/保存接口：

- `GET /api/v1/admin/site-settings`
- `PATCH /api/v1/admin/site-settings`

相关字段：

- `ytdlp_param_mode`: `"safe" | "advanced"`
- `ytdlp_safe`: 结构化参数对象（仅 safe 模式）
- `ytdlp_metadata_args_raw`: metadata 阶段原始参数（仅 advanced 模式）
- `ytdlp_download_args_raw`: download 阶段原始参数（仅 advanced 模式）

任务观测字段（只读）：

- `GET /api/v1/imports`
- `GET /api/v1/imports/{jobId}`
- 返回 `ytdlp_param_mode`，用于确认任务创建时使用的参数模式。

## 4. 安全模式（`ytdlp_param_mode=safe`）

### 4.1 字段定义

`ytdlp_safe` 支持以下字段：

- `format`: 下载格式（映射到 `--format`，仅 download 阶段）
- `extractor_args`: 提取器参数（映射到 `--extractor-args`）
- `user_agent`: UA（映射到 `--user-agent`）
- `referer`: 来源页（映射到 `--referer`）
- `headers`: 额外请求头（映射到 `--add-header "Key: Value"`）
- `socket_timeout`: socket 超时秒数（映射到 `--socket-timeout`，允许 `0-3600`）

### 4.2 示例配置

```json
{
  "ytdlp_param_mode": "safe",
  "ytdlp_safe": {
    "format": "bv*+ba/b",
    "extractor_args": "",
    "user_agent": "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/141.0.0.0 Safari/537.36",
    "referer": "https://example.com/",
    "headers": {
      "Accept-Language": "zh-CN,zh;q=0.9,en;q=0.8"
    },
    "socket_timeout": 30
  }
}
```

### 4.3 适用建议

- 大多数场景优先使用 `safe` 模式。
- 仅当 `safe` 字段不能覆盖需求时再切换 `advanced`。

## 5. 高级模式（`Metadata Args Raw` / `Download Args Raw`）

高级模式允许直接输入参数字符串，由后端解析为 token 数组后执行。

- `ytdlp_metadata_args_raw`：只用于 metadata 探测阶段（`--dump-single-json`）。
- `ytdlp_download_args_raw`：只用于实际下载阶段。

### 5.1 两段参数的区别

- metadata 用于“先识别视频和流信息”。
- download 用于“真正下载媒体文件”。

很多站点要求两阶段身份一致，建议两边都设置相同 `UA/Referer/Headers/Proxy`。

### 5.2 示例配置

```json
{
  "ytdlp_param_mode": "advanced",
  "ytdlp_metadata_args_raw": "--user-agent \"Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/141.0.0.0 Safari/537.36\" --referer \"https://example.com/\" --add-header \"Accept-Language: zh-CN,zh;q=0.9,en;q=0.8\" --proxy \"http://127.0.0.1:7890\"",
  "ytdlp_download_args_raw": "--user-agent \"Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/141.0.0.0 Safari/537.36\" --referer \"https://example.com/\" --add-header \"Accept-Language: zh-CN,zh;q=0.9,en;q=0.8\" --proxy \"http://127.0.0.1:7890\" --format \"bv*+ba/b\""
}
```

### 5.3 参数书写建议

- 使用双引号包裹带空格的参数值。
- 避免粘贴 shell 脚本语法（如 `&&`, `|`, 重定向）。
- 高级模式也会经过危险参数拦截，不是“无限制模式”。

## 6. 默认参数说明（自动附加）

无论是否配置自定义参数，系统都会附加基础参数。  
你不需要手动重复填写这些默认项。

metadata 阶段基础参数：

```bash
yt-dlp --dump-single-json --no-playlist --no-warnings [Metadata Args Raw] <url>
```

download 阶段基础参数：

```bash
yt-dlp --no-playlist --no-progress --no-warnings --restrict-filenames -o <固定模板> [Download Args Raw] <url>
```

说明：

- `-o` 输出模板由系统控制，保证下载文件路径可控。
- 你的自定义参数是“附加参数”。

## 7. 危险参数拦截清单

为避免命令注入和路径逃逸，以下参数会被拒绝：

- `--exec`
- `--exec-before-download`
- `-o`
- `--output`
- `-p`
- `--paths`
- `--config-locations`
- `--batch-file`

包含等号形式也会被拦截，例如：

- `--output=/tmp/x`
- `--exec=...`

## 8. 网络参数示例（含 `--proxy`）

`--proxy` 当前支持在 **advanced 模式**使用。

示例：

```text
--proxy "http://127.0.0.1:7890"
```

SOCKS5 示例：

```text
--proxy "socks5://127.0.0.1:1080"
```

带账号示例：

```text
--proxy "http://user:pass@host:port"
```

建议：

- metadata/download 两段都配置同一代理，避免行为不一致。

## 9. 常见报错与排查

### 9.1 管理端保存时报 400

常见原因：

- `ytdlp_param_mode` 非法。
- 参数字符串无法解析（引号未闭合）。
- 使用了被拦截参数（如 `--output`）。
- `safe` 模式字段不合法（例如 `socket_timeout` 超范围）。

处理建议：

- 从最小配置开始，逐步增加参数。
- 先验证 `safe` 模式是否可满足需求。

### 9.2 导入任务失败：`import.ytdlp.invalid_args`

说明：

- 任务快照中的参数无法被 Worker 接受（通常是历史脏配置或手工写入导致）。

处理建议：

- 修正站点设置后，重新创建新任务。
- 旧任务不会自动修复，需要重新发起导入。

### 9.3 `yt-dlp metadata failed` / `yt-dlp download failed`

说明：

- 参数校验通过，但目标站点请求或下载失败（网络、站点限制、超时、权限等）。

处理建议：

- 检查 UA/Referer/Headers/Proxy 是否与目标站点要求一致。
- 适当提高 `socket_timeout` 或优化代理稳定性。
- 查看任务错误信息与服务器日志中的 `yt-dlp` 输出片段。

## 10. 最佳实践与安全建议

- 优先 `safe` 模式，降低误配置风险。
- 高级模式只添加必要参数，不要一次性堆叠过多选项。
- 不要在参数中放长期有效的敏感凭据；如必须使用，建议短期轮换。
- 配置变更建议记录“变更原因 + 生效时间”，便于回溯任务差异。
- 每次改动后先创建 1 个测试 URL 任务验证，再大规模使用。

## 附录：三套推荐模板

### A. 最小可用（安全模式）

```json
{
  "ytdlp_param_mode": "safe",
  "ytdlp_safe": {
    "user_agent": "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/141.0.0.0 Safari/537.36"
  }
}
```

### B. 通用站点模板（安全模式）

```json
{
  "ytdlp_param_mode": "safe",
  "ytdlp_safe": {
    "format": "bv*+ba/b",
    "user_agent": "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/141.0.0.0 Safari/537.36",
    "referer": "https://example.com/",
    "headers": {
      "Accept-Language": "zh-CN,zh;q=0.9,en;q=0.8"
    },
    "socket_timeout": 30
  }
}
```

### C. 高级模板（metadata/download 分离 + format + proxy）

```json
{
  "ytdlp_param_mode": "advanced",
  "ytdlp_metadata_args_raw": "--user-agent \"Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/141.0.0.0 Safari/537.36\" --referer \"https://example.com/\" --add-header \"Accept-Language: zh-CN,zh;q=0.9,en;q=0.8\" --proxy \"http://127.0.0.1:7890\"",
  "ytdlp_download_args_raw": "--user-agent \"Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/141.0.0.0 Safari/537.36\" --referer \"https://example.com/\" --add-header \"Accept-Language: zh-CN,zh;q=0.9,en;q=0.8\" --proxy \"http://127.0.0.1:7890\" --format \"bv*+ba/b\""
}
```

