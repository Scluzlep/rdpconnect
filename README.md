# 临时端口放行 Web UI

## 1. 项目说明
本项目是一个轻量级、现代化 UI 的网页工具，用于临时放行当前访问者的真实 IP 到指定端口（33899 和 33889），并能够根据连接活跃情况自动回收防火墙规则。该工具基于 Go 语言开发，前端页面直接内嵌在单个二进制文件中，无需数据库和其他复杂依赖，状态持久化到本地 JSON 文件中。

本服务默认不做访问鉴权，请部署在已有 OIDC、SSO、Zero Trust、网关或其他访问控制系统之后。

## 2. 功能说明
- 自动识别当前访问者的真实 IP。
- 仅通过网页按钮即可针对当前 IP 开启或关闭 33899 和 33889 端口。
- 每次开启端口均同时放行对应的 TCP 和 UDP 协议。
- 后台每 10 秒自动监测端口连接状态，当无连接活跃时，延时 3 分钟自动回收放行规则。
- 可选开启 180 分钟“最长放行”保护，超时强制关闭。
- 提供“一键关闭全部”功能。
- 所有业务 iptables 规则均隔离在独立的专属链中，不污染系统原有 INPUT 规则。

## 3. 端口控制逻辑
- **端口限制**：系统仅允许管理 33899 和 33889 两个端口，不能传入任意端口。
- **协议联动**：对某一端口的开启和关闭操作，将严格保持 TCP 和 UDP 的同步。
- **状态维护**：系统以“端口”为单位进行状态跟踪，连接是否断开综合考量 TCP 和 UDP 两个协议的活跃度。

## 4. API 列表
系统提供无传参设计，IP 获取完全由后端依赖请求头与连接信息自动完成：
- `GET /api/status`：获取当前访问者 IP 及其关联端口的规则状态。
- `POST /api/open1`：开启 33899 端口。
- `POST /api/close1`：关闭 33899 端口。
- `POST /api/open2`：开启 33889 端口。
- `POST /api/close2`：关闭 33889 端口。
- `POST /api/close-all`：一键关闭当前 IP 的所有受控端口。
- `POST /api/max-duration/enabled`：开启最长放行（默认 180 分钟）机制。
- `POST /api/max-duration/disabled`：关闭最长放行机制。

## 5. 环境变量说明
- `APP_HOST`：监听地址，默认 `127.0.0.1`。
- `APP_PORT`：监听端口，默认 `8080`。
- `STATE_FILE`：状态持久化文件路径，默认 `./state.json`。
- `IPTABLES_CHAIN`：iptables 专用链名称，默认 `RDP_JIFANG`。
- `MAX_DURATION_MINUTES`：最长放行时间限制，默认 `180`（分钟）。
- `IDLE_CLOSE_MINUTES`：连接空闲等待自动关闭时间，默认 `3`（分钟）。
- `INITIAL_GRACE_MINUTES`：端口开启后的防自动关闭初始宽限期，默认 `10`（分钟）。
- `SCAN_INTERVAL_SECONDS`：后台连接检测周期，默认 `10`（秒）。
- `TRUST_PROXY`：是否信任代理透传的真实 IP（如 `X-Forwarded-For` 等），默认 `true`。

## 6. 编译方式
在项目根目录下执行以下命令，即可编译出单体可执行文件：
```bash
go build -o temp-port-ui main.go
```

## 7. 运行方式
可以直接执行编译好的二进制文件：
```bash
./temp-port-ui
```
（注意：由于程序需要调用 `iptables`、`ss` 和 `conntrack`，建议使用 root 权限或 sudo 运行。）

## 8. systemd 部署方式
1. 将二进制文件拷贝至 `/opt/temp-port-ui/` 目录下。
2. 拷贝本仓库提供的 `systemd/temp-port-ui.service` 至 `/etc/systemd/system/` 目录。
3. 执行如下命令启用并启动服务：
   ```bash
   systemctl daemon-reload
   systemctl enable --now temp-port-ui.service
   systemctl status temp-port-ui.service
   ```

## 9. iptables 权限说明
本程序在运行时会调用系统底层的 `iptables` 命令修改防火墙规则，以及调用 `ss`、`conntrack` 查看连接状态。请确保：
- 服务以 `root` 用户运行。
- 或者通过配置特定的受限 `sudo` 权限使程序可无密码调用上述三项命令。

## 10. iptables 专用规则链说明
为避免杂乱并确保安全，所有由本程序控制的放行规则**均必须**放入专用链（默认名 `RDP_JIFANG`）中，绝不直接写入 `INPUT` 链。
程序启动时会自动执行检查，如果不存在则会自动创建专用链，并向 `INPUT` 链中插入一条指向该专用链的跳转规则。

## 11. 专用链审查命令
运维人员可通过以下命令直接审查该专用链当前的所有放行规则，不影响系统其他防火墙配置：
```bash
iptables -S RDP_JIFANG
iptables -L RDP_JIFANG -n -v --line-numbers
```

## 12. 专用链快速清理命令
当发生突发情况，或需要清理所有程序创建的规则时，使用以下命令可快速清空：
```bash
iptables -F RDP_JIFANG
```
如需完整移除（请确保业务停止或不需要）：
```bash
iptables -D INPUT -j RDP_JIFANG
iptables -F RDP_JIFANG
iptables -X RDP_JIFANG
```

## 13. 真实 IP 获取说明
后端根据请求严格按以下顺序获取唯一的 IPv4：
1. `CF-Connecting-IP`
2. `X-Real-IP`
3. `X-Forwarded-For` 的第一个非空 IP
4. 原始网络连接的 `RemoteAddr`
注意：如果在前端没有合法的 IPv4 地址将返回错误。

## 14. 外部 OIDC 访问控制说明
本系统未包含独立的登录和权限管理功能，仅提供核心授权及放行机制。在实际生产应用中，**强烈建议**在 Nginx 网关、Cloudflare Access、Zero Trust 平台或其它 OIDC 鉴权代理后使用，以实现对使用者的安全身份校验。

## 15. 连接检测说明
系统每 10 秒在后台进行一次扫描：
- **TCP 检测**：使用 `ss -tn` 检查指定 IP 与端口间是否存留相关连接。
- **UDP 检测**：优先使用 `conntrack -L -p udp`（查状态机）；如果该工具不可用，自动降级为使用 `ss -un`。

## 16. 10 分钟初始宽限期说明
当用户从页面手动“开启端口”后，该端口即进入 10 分钟的宽限期（Grace Period）。在这 10 分钟内，即使系统未检测到任何相关的 TCP 或 UDP 连接，也不会触发“空闲自动关闭”动作，保障用户充足的连接和重连时间。

## 17. 3 分钟 idle 自动关闭说明
宽限期结束后，只要当前 IP 与特定端口在 TCP 与 UDP 的总状态为“断开（无活跃连接）”，该端口就正式进入 Idle 倒计时状态。如这种“断开”状态持续 3 分钟以上，系统会自动触发收回，同时关闭该端口的 TCP 和 UDP 放行规则。

## 18. 180 分钟最长放行机制说明
为防止某些长连接导致规则迟迟不被回收，系统支持最长放行时间限制（默认开启）。一旦某个端口的开启时间超过 180 分钟，无论当时是否仍有连接在进行，该规则都将被强行回收并关闭。此功能可在 Web 页面上手动关闭或重新开启。

## 19. TCP/UDP 同端口联动关闭说明
端口被标记为断开并被系统自动关闭时，系统会严苛遵守联动规则：
1. 将通过 `iptables -D` 分别删除对应 TCP 和 UDP 的规则记录。
2. 任何单一协议的存在，都能阻止该端口规则进入空闲倒计时流程。

## 20. 故障排查
1. **获取不到真实 IP**：请检查前端代理（Nginx 等）是否正确配置了 `proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;`。
2. **规则没添加上**：请查看终端日志，确认进程是否有 `iptables` 执行权限。
3. **UDP 空闲检测不准**：确认系统内核加载了 conntrack，并安装了 `conntrack-tools`，这会大幅改善 UDP 连接状态跟踪精确度。

## 21. API 测试命令
在本地或者部署后，您可以通过 curl 快速测试接口响应（请将 `http://127.0.0.1:8080` 替换为实际地址）：
```bash
# 获取状态
curl -s http://127.0.0.1:8080/api/status | jq

# 开启 33899 端口
curl -X POST http://127.0.0.1:8080/api/open1 | jq

# 开启 33889 端口
curl -X POST http://127.0.0.1:8080/api/open2 | jq

# 一键关闭
curl -X POST http://127.0.0.1:8080/api/close-all | jq
```
