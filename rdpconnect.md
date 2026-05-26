# 临时端口放行 Web UI 项目 Prompt

你是一名资深全栈工程师和 Linux 运维安全工程师。请帮我设计并实现一个轻量、现代化 UI 的网页工具，用于临时放行当前访问者真实 IP 到指定端口，并自动回收防火墙规则。

## 一、项目目标

实现一个 Web 管理页面，用于：

1. 获取当前访问网页的客户端真实 IP。
2. 支持反向代理环境下获取真实 IP。
3. 使用 `iptables` 针对当前访问者 IP 临时放行指定端口。
4. 只管理两个端口：
   - `33899`
   - `33889`
5. 每个端口是一个整体开关。
6. 开启端口时，同时放行该端口的 TCP 和 UDP。
7. 关闭端口时，同时关闭该端口的 TCP 和 UDP。
8. 不提供单独开启或关闭 TCP/UDP 的功能。
9. 提供无传参 API，后端自动识别当前访问 IP。
10. 监控同端口 TCP/UDP 连接情况。
11. 同端口 TCP 和 UDP 都消失后，等待 3 分钟，自动关闭该端口。
12. 端口开启后的前 10 分钟内忽略自动关闭检测。
13. 支持网页手动立即关闭端口。
14. 支持一键关闭当前 IP 的全部端口。
15. 提供最长放行时间限制，默认 180 分钟。
16. 180 分钟最长放行机制可以在网页手动开启或关闭。
17. 不需要内置鉴权。
18. 不需要登录页面。
19. 不需要 JWT。
20. 鉴权由外部 OIDC、反向代理、网关或 Zero Trust 平台完成。
21. 页面不要出现“风险”“危险”“安全风险”“警告”等提示词。
22. `iptables` 必须新建专用规则链，所有本工具创建的规则都放到专用链中，便于审查、排障和突发情况下快速清理。

## 二、技术栈要求

优先使用：

- 后端：Go
- 前端：原生 HTML + CSS + JavaScript
- 部署：单个二进制文件
- 前端页面内嵌到 Go 后端中

要求：

- 默认使用 `iptables`
- 不需要 Dockerfile
- 不需要 Nginx 示例
- 不需要内置鉴权
- 不需要登录功能
- 不需要 JWT 解析
- 不需要数据库
- 使用 JSON 文件持久化状态

## 三、真实 IP 获取逻辑

后端必须根据请求自动识别真实客户端 IP。

获取顺序：

1. `CF-Connecting-IP`
2. `X-Real-IP`
3. `X-Forwarded-For` 的第一个 IP
4. 请求连接的 `RemoteAddr`

要求：

- 仅支持 IPv4。
- 必须校验 IP 合法性。
- 不允许前端传入 IP。
- 不允许 API 参数传入 IP。
- 所有 API 都根据当前请求自动识别 IP。
- 如果无法获取合法 IPv4，则返回错误。

## 四、iptables 专用规则链设计

必须新建一个专用 `iptables` 规则链，所有由本程序创建的放行规则都必须写入该链，不允许直接把业务放行规则散落在 `INPUT` 链中。

默认链名：

```text
RDP_JIFANG
```

允许通过环境变量配置：

```env
IPTABLES_CHAIN=RDP_JIFANG
```

### 1. 启动时初始化链

程序启动时必须执行链初始化逻辑：

1. 检查专用链是否存在。
2. 如果不存在，则创建专用链。
3. 检查 `INPUT` 链是否已经跳转到专用链。
4. 如果没有跳转规则，则插入一条跳转规则。
5. 跳转规则应尽量靠前，但不要破坏系统已有关键规则。

示例命令：

```bash
iptables -N RDP_JIFANG
iptables -C INPUT -j RDP_JIFANG
iptables -I INPUT -j RDP_JIFANG
```

Go 中必须使用参数数组执行，不允许 shell 拼接：

```go
exec.Command("iptables", "-N", chain)
exec.Command("iptables", "-C", "INPUT", "-j", chain)
exec.Command("iptables", "-I", "INPUT", "-j", chain)
```

### 2. 规则必须写入专用链

开启端口时，不要直接写入 `INPUT` 链，而是写入专用链。

例如开启 `33899`：

```bash
iptables -I RDP_JIFANG -p tcp -s 当前IP --dport 33899 -j ACCEPT
iptables -I RDP_JIFANG -p udp -s 当前IP --dport 33899 -j ACCEPT
```

关闭端口时，从专用链删除对应规则：

```bash
iptables -D RDP_JIFANG -p tcp -s 当前IP --dport 33899 -j ACCEPT
iptables -D RDP_JIFANG -p udp -s 当前IP --dport 33899 -j ACCEPT
```

检查规则时，也必须检查专用链：

```bash
iptables -C RDP_JIFANG -p tcp -s 当前IP --dport 33899 -j ACCEPT
iptables -C RDP_JIFANG -p udp -s 当前IP --dport 33899 -j ACCEPT
```

### 3. 专用链审查能力

README 和日志中需要说明如何审查当前规则：

```bash
iptables -S RDP_JIFANG
iptables -L RDP_JIFANG -n -v --line-numbers
```

页面可以展示当前链名，例如：

```text
规则链：RDP_JIFANG
```

但页面不要显示风险、危险、警告类文案。

### 4. 突发情况快速清理

README 中需要提供专用链清理命令，方便突发情况下快速处理。

示例：

```bash
iptables -F RDP_JIFANG
```

如果需要完整移除本程序链：

```bash
iptables -D INPUT -j RDP_JIFANG
iptables -F RDP_JIFANG
iptables -X RDP_JIFANG
```

注意：程序自身不要在正常运行时随意删除专用链，只删除自己管理的具体 IP + 端口规则。只有在显式清理命令或文档说明中才提供完整移除方式。

### 5. 链一致性检查

后台扫描或程序启动时需要检查：

- 专用链是否存在。
- `INPUT` 链是否存在跳转到专用链的规则。
- 状态文件中的规则是否存在于专用链。
- 如果状态存在但专用链规则不存在，则清理状态或自动补齐。
- 如果专用链不存在，应自动重建。
- 如果 `INPUT -> RDP_JIFANG` 跳转不存在，应自动补齐。

所有链初始化、检查、补齐、规则添加、规则删除操作都必须写日志。

## 五、端口控制规则

只允许管理以下端口：

```text
33899
33889
```

端口与 API 对应关系：

```text
open1  -> 开启 33899
close1 -> 关闭 33899

open2  -> 开启 33889
close2 -> 关闭 33889
```

### 开启 33899

执行：

```bash
iptables -I RDP_JIFANG -p tcp -s 当前IP --dport 33899 -j ACCEPT
iptables -I RDP_JIFANG -p udp -s 当前IP --dport 33899 -j ACCEPT
```

### 关闭 33899

执行：

```bash
iptables -D RDP_JIFANG -p tcp -s 当前IP --dport 33899 -j ACCEPT
iptables -D RDP_JIFANG -p udp -s 当前IP --dport 33899 -j ACCEPT
```

### 开启 33889

执行：

```bash
iptables -I RDP_JIFANG -p tcp -s 当前IP --dport 33889 -j ACCEPT
iptables -I RDP_JIFANG -p udp -s 当前IP --dport 33889 -j ACCEPT
```

### 关闭 33889

执行：

```bash
iptables -D RDP_JIFANG -p tcp -s 当前IP --dport 33889 -j ACCEPT
iptables -D RDP_JIFANG -p udp -s 当前IP --dport 33889 -j ACCEPT
```

注意：

- 开启端口时，TCP 和 UDP 必须一起开启。
- 关闭端口时，TCP 和 UDP 必须一起关闭。
- 自动关闭端口时，TCP 和 UDP 必须一起关闭。
- 不允许单独关闭 TCP。
- 不允许单独关闭 UDP。
- 端口状态以端口为单位，不以协议为单位。
- 所有规则都必须位于专用链中。

## 六、API 设计

所有 API 都不允许传入 IP。

不要使用以下形式：

```text
/api/open/33899/tcp
/api/close/33899/tcp
/api/port/close
```

必须使用以下 API。

### 获取状态

```http
GET /api/status
```

返回示例：

```json
{
  "success": true,
  "message": "ok",
  "data": {
    "ip": "1.2.3.4",
    "iptablesChain": "RDP_JIFANG",
    "ports": {
      "33899": {
        "enabled": true,
        "tcpAllowed": true,
        "udpAllowed": true,
        "tcpConnected": true,
        "udpConnected": false,
        "connected": true,
        "openedAt": "2026-01-01T12:00:00Z",
        "lastSeenAt": "2026-01-01T12:05:00Z",
        "idleSince": null,
        "autoCloseAt": null,
        "graceUntil": "2026-01-01T12:10:00Z",
        "maxExpireAt": "2026-01-01T15:00:00Z"
      },
      "33889": {
        "enabled": false,
        "tcpAllowed": false,
        "udpAllowed": false,
        "tcpConnected": false,
        "udpConnected": false,
        "connected": false,
        "openedAt": null,
        "lastSeenAt": null,
        "idleSince": null,
        "autoCloseAt": null,
        "graceUntil": null,
        "maxExpireAt": null
      }
    },
    "settings": {
      "maxDurationProtection": true,
      "maxDurationMinutes": 180,
      "idleCloseMinutes": 3,
      "initialGraceMinutes": 10,
      "scanIntervalSeconds": 10,
      "refreshSeconds": 5
    }
  }
}
```

### 开启 33899

```http
POST /api/open1
```

行为：

- 自动识别当前请求 IP。
- 同时放行 TCP 33899 和 UDP 33899。
- 添加前检查专用链内规则是否已存在。
- 不重复添加规则。
- 记录开启时间。
- 设置：

```text
graceUntil = openedAt + 10 分钟
```

- 开启后的 10 分钟内忽略 idle 自动关闭检测。
- 返回最新状态。

### 关闭 33899

```http
POST /api/close1
```

行为：

- 自动识别当前请求 IP。
- 同时关闭 TCP 33899 和 UDP 33899。
- 删除前检查专用链内规则是否存在。
- 规则不存在时忽略。
- 清理 33899 对应状态。
- 返回最新状态。

### 开启 33889

```http
POST /api/open2
```

行为：

- 自动识别当前请求 IP。
- 同时放行 TCP 33889 和 UDP 33889。
- 添加前检查专用链内规则是否已存在。
- 不重复添加规则。
- 记录开启时间。
- 设置：

```text
graceUntil = openedAt + 10 分钟
```

- 开启后的 10 分钟内忽略 idle 自动关闭检测。
- 返回最新状态。

### 关闭 33889

```http
POST /api/close2
```

行为：

- 自动识别当前请求 IP。
- 同时关闭 TCP 33889 和 UDP 33889。
- 删除前检查专用链内规则是否存在。
- 规则不存在时忽略。
- 清理 33889 对应状态。
- 返回最新状态。

### 一键关闭全部

```http
POST /api/close-all
```

行为：

- 自动识别当前请求 IP。
- 同时关闭当前 IP 的：
  - TCP 33899
  - UDP 33899
  - TCP 33889
  - UDP 33889
- 只删除专用链中当前 IP 对应的规则。
- 清理当前 IP 的所有端口状态。
- 返回最新状态。

### 开启 180 分钟最长放行机制

```http
POST /api/max-duration/enabled
```

行为：

- 开启最长放行时间限制。
- 默认最长放行时间为 180 分钟。
- 返回最新状态。

### 关闭 180 分钟最长放行机制

```http
POST /api/max-duration/disabled
```

行为：

- 关闭最长放行时间限制。
- 页面只显示该机制当前为关闭状态。
- 不显示风险提示词。
- 不显示警告文案。
- 返回最新状态。

## 七、统一 API 返回格式

成功：

```json
{
  "success": true,
  "message": "ok",
  "data": {}
}
```

失败：

```json
{
  "success": false,
  "message": "error message"
}
```

## 八、iptables 管理要求

默认使用 `iptables`。

必须安全执行系统命令。

要求：

- 不允许使用 shell 字符串拼接。
- 必须使用参数数组执行命令。
- 所有输入都必须校验。
- IP 必须是合法 IPv4。
- 端口只能是白名单：
  - `33899`
  - `33889`
- 协议只能是：
  - `tcp`
  - `udp`
- 链名必须做合法性校验，只允许字母、数字、下划线、短横线，长度合理。
- 添加规则前，先检查规则是否存在。
- 规则存在则不重复添加。
- 删除规则前，先检查规则是否存在。
- 规则不存在则忽略。
- 所有添加、删除、检查操作都记录日志。
- 所有业务放行规则必须在专用链中。
- 程序启动时自动确保 `INPUT` 链跳转到专用链。

检查规则：

```bash
iptables -C RDP_JIFANG -p tcp -s 1.2.3.4 --dport 33899 -j ACCEPT
```

添加规则：

```bash
iptables -I RDP_JIFANG -p tcp -s 1.2.3.4 --dport 33899 -j ACCEPT
```

删除规则：

```bash
iptables -D RDP_JIFANG -p tcp -s 1.2.3.4 --dport 33899 -j ACCEPT
```

Go 中必须使用类似下面的方式执行：

```go
exec.Command("iptables", "-C", chain, "-p", "tcp", "-s", ip, "--dport", port, "-j", "ACCEPT")
```

不要使用：

```go
exec.Command("sh", "-c", "iptables ...")
```

## 九、连接监控逻辑

后台每 10 秒扫描一次已开启端口。

### TCP 检测

使用：

```bash
ss -tn
```

判断当前 IP 与目标端口是否存在 TCP 连接。

判断条件：

- 本地端口是目标端口，且远端 IP 是当前 IP；
- 或远端端口是目标端口，且远端 IP 是当前 IP。

### UDP 检测

UDP 没有严格意义上的长连接。

优先使用：

```bash
conntrack -L -p udp
```

如果 `conntrack` 不可用，则降级使用：

```bash
ss -un
```

判断是否存在当前 IP 与目标 UDP 端口的近期记录。

## 十、同端口 TCP/UDP 联动逻辑

这是核心规则，必须严格实现。

每个端口只维护一个整体状态。

综合连接状态：

```text
connected = tcpConnected || udpConnected
```

### 情况 1：UDP 消失，但 TCP 仍然存在

例如 `33899`：

```text
udpConnected = false
tcpConnected = true
connected = true
```

结果：

- `33899` 仍然认为有连接。
- 不进入 idle 倒计时。
- 不关闭端口。
- TCP 和 UDP 都保持放行。

### 情况 2：TCP 消失，但 UDP 仍然存在

例如 `33899`：

```text
tcpConnected = false
udpConnected = true
connected = true
```

结果：

- `33899` 仍然认为有连接。
- 不进入 idle 倒计时。
- 不关闭端口。
- TCP 和 UDP 都保持放行。

### 情况 3：TCP 和 UDP 都消失

例如 `33899`：

```text
tcpConnected = false
udpConnected = false
connected = false
```

结果：

- `33899` 开始进入 idle 倒计时。
- idle 超过 3 分钟后，同时关闭：
  - TCP 33899
  - UDP 33899

### 情况 4：手动关闭端口

例如关闭 `33899`：

```http
POST /api/close1
```

结果：

- 同时关闭：
  - TCP 33899
  - UDP 33899
- 清理该端口状态。

### 情况 5：自动关闭端口

例如 `33889` 达到自动关闭条件：

结果：

- 同时关闭：
  - TCP 33889
  - UDP 33889
- 清理该端口状态。

## 十一、自动关闭机制

每个端口状态需要记录：

```text
ip
port
enabled
tcpAllowed
udpAllowed
tcpConnected
udpConnected
connected
openedAt
lastSeenAt
idleSince
graceUntil
maxExpireAt
```

### 1. 初始宽限期

端口开启后 10 分钟内忽略 idle 自动关闭检测。

```text
graceUntil = openedAt + 10 分钟
```

在宽限期内：

- 仍然检测 TCP 状态。
- 仍然检测 UDP 状态。
- 仍然更新 connected。
- 如果 connected 为 true，更新 lastSeenAt。
- 不设置 idleSince。
- 不执行 idle 自动关闭。
- 允许手动关闭。
- 仍然受 180 分钟最长放行机制影响。

### 2. idle 自动关闭

宽限期结束后：

```text
connected = tcpConnected || udpConnected
```

如果：

```text
connected = true
```

则：

- 更新 lastSeenAt。
- 清空 idleSince。
- 不关闭端口。

如果：

```text
connected = false
```

则：

- 如果 idleSince 为空，设置 idleSince 为当前时间。
- 如果当前时间 - idleSince >= 3 分钟，则自动关闭该端口。
- 自动关闭时必须同时关闭该端口 TCP 和 UDP。

### 3. 最长放行机制

默认开启。

默认最长放行时间：

```text
180 分钟
```

如果最长放行机制开启：

```text
if now >= openedAt + 180 分钟:
    同时关闭该端口 TCP 和 UDP
```

如果最长放行机制关闭：

- 不执行 180 分钟自动关闭。
- 页面只显示该机制为关闭状态。
- 不显示任何风险、危险、警告类文案。

## 十二、状态存储

使用 JSON 文件持久化状态。

环境变量：

```env
STATE_FILE=./state.json
```

状态文件示例：

```json
{
  "rules": [
    {
      "ip": "1.2.3.4",
      "port": 33899,
      "enabled": true,
      "tcpAllowed": true,
      "udpAllowed": true,
      "tcpConnected": true,
      "udpConnected": false,
      "connected": true,
      "openedAt": "2026-01-01T12:00:00Z",
      "lastSeenAt": "2026-01-01T12:05:00Z",
      "idleSince": null,
      "graceUntil": "2026-01-01T12:10:00Z",
      "maxExpireAt": "2026-01-01T15:00:00Z"
    }
  ],
  "settings": {
    "iptablesChain": "RDP_JIFANG",
    "maxDurationProtection": true,
    "maxDurationMinutes": 180,
    "idleCloseMinutes": 3,
    "initialGraceMinutes": 10,
    "scanIntervalSeconds": 10
  }
}
```

要求：

- 每次规则变化后保存状态。
- 使用临时文件 + rename 写入，避免状态文件损坏。
- 服务启动时读取状态文件。
- 服务启动时初始化并检查专用链。
- 服务启动时检查状态中的 iptables 规则是否还存在于专用链。
- 如果状态存在但 iptables 规则不存在，则清理状态或自动补齐。
- 如果 TCP 或 UDP 其中一条规则缺失，可以自动补齐，保证端口整体状态一致。
- 状态读写必须加锁，避免并发问题。

## 十三、环境变量

支持以下环境变量：

```env
APP_HOST=0.0.0.0
APP_PORT=8080
STATE_FILE=./state.json

IPTABLES_CHAIN=RDP_JIFANG

MAX_DURATION_MINUTES=180
IDLE_CLOSE_MINUTES=3
INITIAL_GRACE_MINUTES=10
SCAN_INTERVAL_SECONDS=10

TRUST_PROXY=true
```

说明：

```text
APP_HOST
监听地址。

APP_PORT
监听端口。

STATE_FILE
状态文件路径。

IPTABLES_CHAIN
iptables 专用规则链名称，默认 RDP_JIFANG。

MAX_DURATION_MINUTES
最长放行时间，默认 180 分钟。

IDLE_CLOSE_MINUTES
同端口 TCP 和 UDP 都消失后的等待关闭时间，默认 3 分钟。

INITIAL_GRACE_MINUTES
端口开启后的初始宽限期，默认 10 分钟。

SCAN_INTERVAL_SECONDS
后台连接检测周期，默认 10 秒。

TRUST_PROXY
是否信任反向代理传入的真实 IP 头。
```

## 十四、前端页面要求

页面使用轻量现代化 UI。

### 页面风格

要求：

- 卡片式布局。
- 响应式布局。
- 现代 Toggle Switch。
- 支持桌面和移动端。
- 可以使用深色主题。
- 不使用大型前端框架。
- 不需要构建步骤。
- 原生 HTML、CSS、JavaScript 即可。

### 页面顶部显示

显示：

- 当前访问 IP。
- 服务状态。
- 当前时间。
- 自动刷新状态。
- 当前 iptables 专用链名。

不要显示：

- 登录入口。
- 用户管理。
- JWT 信息。
- 鉴权配置。
- 风险提示。
- 警告提示。

### 端口卡片：33899

显示：

- 端口：33899。
- 总开关状态。
- TCP 放行状态。
- UDP 放行状态。
- TCP 连接状态。
- UDP 连接状态。
- 综合连接状态。
- 开启时间。
- 最近连接时间。
- 宽限期结束时间。
- idle 自动关闭倒计时。
- 最长放行到期时间。

按钮：

```text
开启 -> POST /api/open1
关闭 -> POST /api/close1
```

### 端口卡片：33889

显示：

- 端口：33889。
- 总开关状态。
- TCP 放行状态。
- UDP 放行状态。
- TCP 连接状态。
- UDP 连接状态。
- 综合连接状态。
- 开启时间。
- 最近连接时间。
- 宽限期结束时间。
- idle 自动关闭倒计时。
- 最长放行到期时间。

按钮：

```text
开启 -> POST /api/open2
关闭 -> POST /api/close2
```

### 全局控制区

包含：

- 一键关闭全部按钮。
- 180 分钟最长放行机制开关。
- 当前 idle 规则说明：同端口 TCP 和 UDP 都消失 3 分钟后自动关闭。
- 当前初始宽限期说明：开启后 10 分钟内忽略 idle 自动关闭检测。
- 当前规则链名称显示。

禁止出现以下词语：

```text
风险
危险
安全风险
警告
Warning
Danger
Risk
```

### 自动刷新

要求：

- 页面每 5 秒调用一次 `GET /api/status`。
- 手动操作后立即刷新状态。
- 操作成功显示 Toast。
- 操作失败显示普通错误 Toast。

## 十五、项目结构

请输出完整项目，结构如下：

```text
temp-port-ui/
├── go.mod
├── main.go
├── README.md
├── systemd/
│   └── temp-port-ui.service
└── examples/
    └── state.example.json
```

不要生成：

```text
Dockerfile
docker-compose.yml
nginx.conf
登录页面
鉴权模块
JWT 模块
```

## 十六、systemd 服务文件

请提供：

```ini
[Unit]
Description=Temporary IP Port Allow Web UI
After=network.target

[Service]
Type=simple
WorkingDirectory=/opt/temp-port-ui
ExecStart=/opt/temp-port-ui/temp-port-ui
Restart=always
Environment=APP_HOST=127.0.0.1
Environment=APP_PORT=8080
Environment=STATE_FILE=/opt/temp-port-ui/state.json
Environment=IPTABLES_CHAIN=RDP_JIFANG
Environment=MAX_DURATION_MINUTES=180
Environment=IDLE_CLOSE_MINUTES=3
Environment=INITIAL_GRACE_MINUTES=10
Environment=SCAN_INTERVAL_SECONDS=10
Environment=TRUST_PROXY=true

[Install]
WantedBy=multi-user.target
```

说明：

- 程序需要具备执行 `iptables`、`ss`、`conntrack` 的权限。
- 可直接以 root 运行。
- 或配置受限 sudo 权限执行相关命令。
- 访问控制由外部 OIDC / 反向代理 / 网关完成。
- 本服务内部不做登录和鉴权。

## 十七、README 要求

README 必须包含：

1. 项目说明。
2. 功能说明。
3. 端口控制逻辑。
4. API 列表。
5. 环境变量说明。
6. 编译方式。
7. 运行方式。
8. systemd 部署方式。
9. iptables 权限说明。
10. iptables 专用规则链说明。
11. 专用链审查命令。
12. 专用链快速清理命令。
13. 真实 IP 获取说明。
14. 外部 OIDC 访问控制说明。
15. 连接检测说明。
16. 10 分钟初始宽限期说明。
17. 3 分钟 idle 自动关闭说明。
18. 180 分钟最长放行机制说明。
19. TCP/UDP 同端口联动关闭说明。
20. 故障排查。
21. API 测试命令。

README 不要包含：

```text
Docker 部署说明
Nginx 示例
内置登录说明
JWT 说明
风险提示文案
```

可以说明：

```text
本服务默认不做访问鉴权，请部署在已有 OIDC、SSO、Zero Trust、网关或其他访问控制系统之后。
```

## 十八、代码质量要求

- 尽量使用单文件 Go 实现。
- 代码清晰。
- 错误处理完整。
- 日志清晰。
- 状态读写加锁。
- 后台扫描逻辑稳定。
- API 返回格式统一。
- 不使用 shell 拼接执行系统命令。
- 不允许外部传入 IP。
- 不允许外部传入任意端口。
- 不允许外部传入任意协议。
- 端口只能是 `33899` 和 `33889`。
- 开启端口时 TCP 和 UDP 一起开启。
- 关闭端口时 TCP 和 UDP 一起关闭。
- 自动关闭端口时 TCP 和 UDP 一起关闭。
- 所有业务规则必须写入专用 iptables 链。
- 程序启动必须确保专用链存在。
- 程序启动必须确保 `INPUT` 链跳转到专用链。
- 自动关闭判断基于：

```text
connected = tcpConnected || udpConnected
```

- 只有 TCP 和 UDP 都消失后才进入 idle 倒计时。
- 端口开启后 10 分钟内忽略 idle 自动关闭检测。
- 180 分钟最长放行机制默认开启。
- 页面不出现风险、危险、警告类文案。

## 十九、最终交付内容

请最终输出：

1. 项目目录结构。
2. `go.mod` 完整内容。
3. `main.go` 完整内容。
4. `README.md` 完整内容。
5. `systemd/temp-port-ui.service` 完整内容。
6. `examples/state.example.json` 完整内容。
7. 编译命令。
8. 运行命令。
9. API 测试命令。
10. 自动关闭逻辑说明。
11. iptables 专用链初始化逻辑说明。
12. iptables 专用链审查和清理命令。

请优先保证功能可用、轻量、易部署，并保证 iptables 操作安全。

重要：不要额外添加 Dockerfile、Nginx 配置、登录页面、鉴权逻辑、JWT 解析、用户系统，也不要在页面或 README 中加入风险、危险、警告类提示词。
