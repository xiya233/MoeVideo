# yt-dlp 用户 Cookies 使用指南

本文档介绍如何在 MoeVideo 中为 URL 导入配置并使用“用户自定义 yt-dlp Cookies”。

## 1. 功能作用

该功能用于提升某些站点 URL 导入成功率，尤其是需要登录态或特定 Cookie 才能访问媒体流的站点。

支持两种 Cookie 格式：

1. `Cookie Header`
2. `cookies.txt`

当前生效范围：

1. 仅 URL 导入生效（不影响 BT 导入）
2. 仅当你在 `/import` 手动选择某条 Cookie 配置时生效
3. 对主链与 fallback 链路都生效（metadata/download 与候选链接下载）

## 2. 在 `/me` 配置 Cookie

路径：`/me` -> `编辑资料` -> `yt-dlp Cookies`

你可以执行：

1. 新增配置
2. 编辑配置
3. 删除配置

每条配置包含字段：

1. `配置名称`：用于你自己识别
2. `域名规则`：例如 `24av.net`
3. `格式`：`Cookie Header` 或 `cookies.txt`
4. `内容`：实际 Cookie 内容（提交后不回显）

注意：

1. 系统不会在列表接口回传明文 Cookie
2. 编辑时是“覆盖写入”模式，需要重新填写内容

## 3. 在 `/import` 使用 Cookie

路径：`/import` -> `URL 导入`

操作步骤：

1. 输入视频页面 URL
2. 在“Cookie 配置（可选）”下拉中选择一条配置
3. 点击“开始导入”

下拉列表只会显示与当前 URL 域名匹配的配置（后端 `for_url` 过滤）。

## 4. 域名匹配规则

`domain_rule` 采用主域 + 子域匹配：

1. 配置 `24av.net` 可匹配 `24av.net` 与 `www.24av.net`
2. 不需要写 `*.`，系统会按主域规则处理

输入会被标准化：

1. 自动转小写
2. 去掉前导 `*.` / `.`
3. 去掉末尾 `.` 和端口

## 5. Cookie 注入优先级

系统注入时遵循“管理员参数优先”：

1. 若管理员 yt-dlp 参数中已显式设置 Cookie 来源（如 `--cookies`、`--cookies-from-browser`、`--add-header Cookie`）
2. 则用户手选 Cookie 不覆盖管理员配置

否则会注入用户 Cookie：

1. `header` -> `--add-header "Cookie: ..."`
2. `cookies_txt` -> 写入任务临时文件后 `--cookies <file>`

## 6. 与 inspect/start 的关系

URL 导入是两段式：

1. `inspect`（探测）
2. `start`（真正创建任务）

如果你选择了 `user_cookie_id`：

1. `inspect` 会带上它参与 metadata 支持检查
2. `start` 会把该 Cookie 的密文快照写入任务，保证任务执行不受你后续修改影响
3. 在候选链接模式下，`inspect_token` 与 `user_cookie_id` 必须一致

## 7. 安全说明

存储方式：

1. Cookie 明文在写入时加密（AES-GCM）
2. 密钥由 `JWT_SECRET` 派生（HKDF）
3. 数据库仅存密文与随机 nonce，不存明文

日志与接口：

1. 列表接口不回显 Cookie 明文
2. 明文仅在新增/编辑提交时经过后端

## 8. 常见问题排查

1. 下拉看不到配置
- 确认 URL 是 `http/https` 且域名与 `domain_rule` 匹配
- 确认配置属于当前登录用户

2. 提示 `user_cookie_id does not match current url domain`
- 说明所选配置不匹配当前 URL 的 host，请换对应域名配置

3. 提示 `user_cookie_id does not match inspect_token`
- 说明 inspect 后又改了下拉选择，需重新 inspect 或保持同一选择再 start

4. 选择后仍像没生效
- 检查管理员后台 yt-dlp 参数是否已设置 Cookie 来源；管理员配置优先

## 9. 建议实践

1. 每个站点单独建一条配置，名称清晰（如：`24av-主账号`）
2. `cookies.txt` 优先用于复杂站点登录态，稳定性通常好于单行 Header
3. Cookie 失效后及时更新（编辑时重新粘贴内容）
