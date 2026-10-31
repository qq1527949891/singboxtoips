# 多 IP 单协议代理节点工具

面向 Linux 服务器，安装入口支持 **Debian 12+ / systemd / amd64、arm64**。通过中文命令行交互选择**一种协议**，每个已配置的 IPv4 地址生成一个独立节点，全部节点统一使用选定协议。节点可在支持该协议的不同代理客户端中使用。

例如选择 Hysteria2、选中 10 个 IP，将生成 10 个 Hysteria2 节点。更换协议时整体替换，工具不会同时启用第二种协议。

支持 **SOCKS5、Shadowsocks、VMess、VLESS、Trojan、Hysteria2、TUIC、WireGuard** 共 8 类。VLESS 使用 REALITY，VMess 和 Trojan 使用 TLS；无需手动选择或安装多个服务端核心。

## 选择建议和菜单顺序

菜单按常见流量识别、主动探测，以及 UDP 受限环境下的抗封锁需求给出以下**工程参考顺序**。这是有条件的选型建议，没有跨网络实测数据支持一个永久、通用的排名：

| 菜单 | 协议 | 抗封锁考虑 | 游戏 UDP 考虑 |
|---|---|---|---|
| 1 | VLESS + REALITY | 默认建议；TLS 外观、未认证探测回落；使用 TCP | 可以代理 UDP，但外层 TCP 的重传等待可能增加抖动 |
| 2 | Trojan + TLS | HTTPS 外观；附带本机网页回落；需匹配域名的可信证书 | UDP 经 TCP 承载 |
| 3 | Hysteria2 | HTTP/3 外观；可选 Salamander 混淆；UDP/QUIC 必须可达 | 游戏优先候选，使用数据报转发；是否更快应实测 |
| 4 | VMess + TLS | TLS 外观与 AEAD；没有独立网页回落 | UDP 经 TCP 承载 |
| 5 | TUIC v5 | TLS/QUIC；没有上述网页回落能力；依赖 UDP | native 数据报转发，关闭 0-RTT |
| 6 | Shadowsocks AEAD | 兼容选项；加密但没有标准 TLS/HTTP 外观 | 直接代理 UDP |
| 7 | WireGuard | 标准协议不提供流量混淆，依赖 UDP | 用户态隧道端点，转发 IPv4 TCP/UDP |
| 8 | SOCKS5 | 用户名/密码认证；账号和流量均不加密，没有伪装 | 使用协商的动态 UDP 中继端口 |

**抗封锁优先时默认选 REALITY；游戏 UDP 优先且网络允许 UDP 时，可选 Hysteria2。** 不自动添加备用协议。Salamander 改变 QUIC 外观，不能把启用混淆后的流量继续描述为标准 HTTP/3 外观。IP 已被封锁时，各协议均无法保证恢复。

设计依据来自项目官方说明，并不把项目的宣传表述当作性能或抗封锁实测结果：

- [REALITY](https://github.com/XTLS/REALITY)
- [sing-box REALITY/TLS](https://sing-box.sagernet.org/configuration/shared/tls/)
- [Hysteria2 协议](https://v2.hysteria.network/docs/developers/Protocol/)
- [TUIC 数据报模式](https://sing-box.sagernet.org/configuration/outbound/tuic/)
- [QUIC 数据报 RFC 9221](https://www.rfc-editor.org/rfc/rfc9221.html)
- [WireGuard 的已知限制](https://www.wireguard.com/known-limitations/)

## Debian 安装

从 GitHub 下载独立安装入口，在服务器上运行：

```sh
curl -fL https://github.com/qq1527949891/singboxtoips/releases/latest/download/install.sh -o install.sh
sudo sh install.sh
```

脚本自动识别 amd64/arm64，查询该仓库最新 Release。如果已安装相同版本，复用现有程序；否则下载对应的 `mip-proxy-linux-<架构>.tar.gz`，核对 Release 中 `SHA256SUMS` 后安装，再进入中文菜单。下载失败或校验不符会终止安装。脚本支持通过 `MIP_VERSION` 固定版本：

```sh
sudo env MIP_VERSION=v0.1.2 sh install.sh
sudo sh install.sh --no-menu
sh install.sh --download-only ./packages
```

`--no-menu` 仅安装管理程序；`--download-only` 下载并校验压缩包，无需 root，不修改服务。仅安装管理程序不会禁用 IPv6；正式部署节点时才执行。

也可以手动从 [Releases](https://github.com/qq1527949891/singboxtoips/releases) 下载对应架构的发布包并上传服务器。例如 amd64：

```sh
tar -xzf mip-proxy-linux-amd64.tar.gz
cd mip-proxy-linux-amd64
sudo sh install.sh
```

包内 `install.sh` 优先使用本地管理程序，不重复下载压缩包。也可以直接运行包内的管理程序：

```sh
sudo ./mip-proxy
```

部署时使用官方 sing-box **1.14.4**，先核对固定 SHA256，再安装到独立路径；不覆盖系统原有的 sing-box 或 Xray。部署包中的管理程序不依赖 Go、Python 或额外运行时。首次下载核心需要连接 GitHub。

交互顺序：

1. 从按抗封锁需求排列的菜单选择**一个**协议。
2. 自动读取网卡 IPv4 与路由，默认使用全部可用 IP；已有节点保留 ID 和凭据。
3. 自动使用监听端口 443（已有部署保留原端口），按网卡/网关选择推荐策略路由。
4. TLS 协议默认自动申请公网 IP 证书并自动续期，无需购买域名或填写 PEM 路径；已有手动证书继续使用。
5. 显示入口/出口摘要后部署。无法可靠判断的 NAT 入站映射、缺失网关等必要信息才会询问。

不同 IP 可共用端口，各节点的入口和出站地址分别绑定。WireGuard 按节点 ID 分配不同 UDP 端口。443 是默认值，不能单靠端口保证抗封锁；已被其他服务占用时不会覆盖或停止该服务。

需要自定义 IP 选择、端口、路由、REALITY 握手目标、证书方式或 Salamander 混淆时使用高级模式；需要预览时明确指定输出目录：

```sh
sudo mip-proxy wizard --advanced
sudo mip-proxy wizard --out /root/mip-preview
```

不带 `--out` 的普通向导默认部署，无需再次选择“应用配置”。高级模式保留自定义和预览选择。

按 Ctrl+C 取消；正式修改发生在配置检查和备份完成之后。部署失败会尝试恢复旧配置、旧凭据及原有服务状态，恢复错误不会隐藏。

## IP 与网络要求

- 仅使用 **IPv4**。正式部署会禁用整台服务器的 IPv6，各节点也拒绝 IPv6 目标。IP 必须事先由云平台分配，并正确配置到本机；工具不会擅自给网卡加地址。
- 支持公网 IP 直接绑定、私网 IP 对应公网 EIP，以及多网卡/多网关。NAT 必须实际支持所需的入站和出站映射。仅有多个公网入站、实际共用一个出站的 NAT，不能由代理配置变成多个公网出口。
- 从指定本机地址查询到的公网 IP，仅代表观测到的出口，不能证明入站 NAT 或安全组已正确配置。
- 每个节点绑定对应本机地址出站。除 WireGuard 外，入站单独监听本机地址，不同 IP 可以复用端口。WireGuard 的核心使用通配 UDP 监听，因此按节点 ID 分配独立端口，借助策略路由固定握手回包的源 IP；请以摘要中列出的 IP:端口为准。
- 单网卡单网关一般只需地址绑定。多网卡/多网关可开启独立路由，生成源地址与 fwmark 两种规则，兼顾代理出站和客户端连接回程。自定义的其他源地址路由需先检查是否与此方案兼容。
- 只操作本工具登记的精确 IPv4 规则/路由，不按编号范围清理，不修改 IPv4 主路由表或 Swap。全局 IPv6 开关的管理见下文。
- 系统防火墙与云安全组需要允许对应端口。工具不会清空或替换现有防火墙。REALITY/Trojan/VMess 为 TCP，Hysteria2/TUIC/WireGuard 为 UDP，Shadowsocks 为 TCP+UDP。
- SOCKS5 的监听端口是 TCP；UDP ASSOCIATE 另行协商动态 UDP 中继端口，仅放行同端口 UDP 不够。可按可信客户端来源放行动态 UDP 端口。NAT 还需保留这些端口，且客户端须能处理服务端返回的私网中继地址；只有固定端口映射时不能保证 SOCKS5 UDP 可用。SOCKS5 不作为抗封锁推荐。
- 同一服务器的多个 IP 通常共享上游线路和带宽，增加 IP 不会自动产生不同的加速线路。

自动探测不适用时，复制 `samples/ip-map.csv` 并填入实际数据：

```sh
sudo mip-proxy wizard --inventory /root/ip-map.csv --apply
```

CSV 表头：`local_ip,public_ip,interface,gateway,prefix`。后 3 项仅在策略路由需要时填写。示例中的地址是文档保留地址，不能直接部署。重复出口会保留为两行待检查，不会覆盖或冒充两个独立公网 IP。

### IPv6 的处理

正式部署设置 `net.ipv6.conf.all.disable_ipv6=1` 和 `net.ipv6.conf.default.disable_ipv6=1`，并验证所有现有接口均已禁用。设置保存到 `/etc/sysctl.d/99-mip-proxy-ipv6.conf`，每次代理服务启动前再次检查，覆盖重启后的运行状态。此操作影响整台服务器，包括原有 IPv6 地址和连接；预览不会修改这些设置。

服务端在节点路由前拒绝 IPv6 目标，DNS 解析和出站使用 IPv4。Mihomo 完整配置设置 `ipv6: false`，sing-box 客户端也使用 IPv4 DNS 策略并拒绝 IPv6。单独的分享链接或 provider 无法控制第三方客户端的全局 IPv6 设置，但服务端仍会拒绝节点内的 IPv6 请求。

部署前备份原有 IPv6 开关、地址和路由。部署失败或执行 `rollback` 时恢复部署前状态；如果部署前已禁用 IPv6，则保持禁用。

## 域名和证书

REALITY 不需要自有证书，需要服务器可访问的 TLS 握手目标；普通向导采用默认目标，SNI、握手目标可在 `--advanced` 中修改。默认目标只是起点，应检查其在目标服务器上的 TLS 兼容性和可达性。

Trojan、VMess、Hysteria2 和 TUIC 使用 TLS 证书证明服务器身份；私钥由服务器保管。默认无需自有域名，工具选取一个节点公网 IPv4，通过 Let's Encrypt 自动申请受信任的 IP 证书。所有节点连接各自公网 IP，共用这一证书身份，客户端配置自动填写验证 IP；出站仍分别绑定各节点地址。

IP 证书使用 `shortlived` 配置，有效期约 6 天，由 sing-box 内置 ACME 管理器自动续期，无需安装 certbot、acme.sh 或设置额外定时任务。使用兼容 Hysteria2 的 P-256 密钥。依据：[Let's Encrypt IP 证书](https://letsencrypt.org/2026/01/15/6day-and-ip-general-availability)、[sing-box ACME](https://sing-box.sagernet.org/configuration/shared/certificate-provider/acme/)。

**证书验证 IP 的 TCP 80 必须允许公网访问，供首次签发和以后续期使用。** 云安全组、防火墙和 NAT 都需放行；与代理使用的 TCP/UDP 443 分开。工具不会清空防火墙，不会停止占用 80 的网站；发现占用会报错并恢复部署前状态。TLS-ALPN 验证关闭，避免与代理的 TCP 443 监听冲突。

部署最多等待 3 分钟，核对缓存证书的可信链、IP 身份和有效期后才导出节点；签发失败会回滚，并给出日志查看命令。证书和 ACME 账户私钥保存在 `/var/lib/mip-proxy/acme`，目录仅供 root 访问，私钥不进入节点导出。核心运行期间自动加载续期证书；保留缓存可避免重复签发。

高级模式也可使用已有 PEM 完整链与私钥，证书需匹配填写的域名或 IP。客户端 TLS 校验保持开启，不生成 `insecure=1` 或 `skip-cert-verify: true`。

Hysteria2 使用 ECDSA 或 RSA 证书，工具会拒绝与当前核心 Chrome 握手模拟不兼容的 Ed25519 证书。

使用已有 PEM 文件时，可由已有 ACME 工具签发与续期。文件更新后，运行以下命令加载新证书；该命令会先验证再替换，节点凭据保持不变。自动 IP 证书模式无需执行此操作：

```sh
sudo mip-proxy apply --state /var/lib/mip-proxy/state.json
```

离线 demo 使用的短期自签证书仅供配置检查/测试，不属于生产证书。

## 节点导出

默认目录 `/var/lib/mip-proxy/export`，目录权限 0700，文件权限 0600：

| 文件 | 用途 |
|---|---|
| `links.txt` | 每行一个分享链接 |
| `subscription.txt` | Base64 分享链接集合；客户端须识别相应协议 |
| `mihomo.yaml` | 可导入的完整 Mihomo 配置，手动选择节点 |
| `mihomo-provider.yaml` | Mihomo proxy-provider 内容 |
| `sing-box.json` | 可导入的 sing-box 客户端配置，手动选择节点 |
| `nodes.csv` | IP、端口、协议及分享链接清单 |
| `<公网IP>-wireguard.conf` | WireGuard 原生客户端配置，仅在选择 WireGuard 时生成 |

WireGuard 没有统一分享 URI，因此该协议不生成 `links.txt`、`subscription.txt`，使用 `.conf` 或 Mihomo/sing-box 配置导入。SOCKS5 的分享链接格式因客户端而异，可优先导入 YAML/JSON 或手动填写账号。更换协议时会移除本工具上次生成的旧导出文件，保留目录中的其他文件。

仅导出节点和普通客户端配置，不配置具体游戏或接管客户机。没有自动公开的订阅 HTTP 服务；导出文件含凭据，请自行安全传输。客户端必须支持所选协议，不能让不支持协议的客户端通过格式转换获得支持。

重新运行向导、增删 IP 或整体更换协议后，已有 IP 的 ID 和凭据会保留。随机凭据独立生成；REALITY 私钥只保存在服务器状态/配置/备份中，不进入客户端导出。

### WireGuard 的具体行为

每个 IP 的 UDP 端口为 `起始端口 + 节点 ID - 1`，删除节点后不会重排已有端口。即使只有一张网卡，也会启用各 IP 的策略路由，以固定 UDP 握手回包的源 IP；向导需获得该 IP 的网卡、网关与网段。核心底层端口使用通配监听，部署时应按导出 IP:端口在防火墙中限制入口。

每个 IP 导出一个客户端 peer，同一配置不要在多台设备同时启用，否则 WireGuard 的端点漫游会互相抢占连接。工具另设独立诊断 peer，运行 `doctor` 不会占用用户的 peer。诊断私钥只保存在服务器，不进入客户端导出。

服务端使用 sing-box 用户态 WireGuard 端点转发 IPv4 TCP/UDP，无需安装内核 WireGuard、开启全局 IP 转发或添加全局 MASQUERADE。此版本不提供完整的内核 VPN、IPv6 或 ICMP 行为保证。

## 状态、检测与回滚

```sh
sudo mip-proxy status
sudo mip-proxy export
sudo mip-proxy doctor
sudo mip-proxy rollback
sudo journalctl -u mip-proxy.service -n 100 --no-pager
```

`doctor` 在服务器本机启动临时客户端，经每个节点的实际协议检查认证、TCP 转发和公网出口；通过 SOCKS5 UDP ASSOCIATE 向公共 STUN 服务发送 3 次探测，检查 UDP 转发及公网出口。进程结束后清理临时客户端。STUN 无响应也可能来自公共服务或上游限制，不直接等价于协议实现有问题。

本机检查不会证明异机到公网入口的可达性；这需要你从其他机器连接。输出的 STUN 往返时延从服务器本机测量，不代表用户到节点的游戏延迟。

部署只管理这些服务：

- `mip-proxy.service`：代理核心。
- `mip-proxy-routes.service`：network-online 后恢复登记的精确策略路由，先于代理核心启动。
- `mip-proxy-cover.service`：Trojan 使用的本机 HTTP 回落页，仅监听 `127.0.0.1:18080`。

配置 `/etc/mip-proxy/server.json`，状态/备份 `/var/lib/mip-proxy`，管理程序 `/usr/local/bin/mip-proxy`，独立核心 `/usr/local/lib/mip-proxy/sing-box`。开机路由恢复只使用状态，不访问原始证书文件。

管理操作通过文件锁串行执行；默认状态的导出和检测也会检查事务锁。部署前持久化恢复记录；中断后下一次 apply/rollback 会先恢复未完成事务。`rollback` 恢复上一次部署前的文件、规则、IPv6 状态和服务启用状态；备份路径和恢复文件均限制在本工具管理范围。清理路由前核对其归属，其他程序更改过的规则和路由会保留并报告冲突。

## 开发与验证

```sh
go test ./...
go vet ./...
python3 scripts/build.py
```

发布使用 Go 1.27.3。打包脚本只在开发机运行，生成 `dist/` 下 amd64/arm64 的静态 Linux 程序、安装压缩包、源码包、独立 `install.sh` 和 `SHA256SUMS`。服务器解压后运行 `install.sh`，不需要 Go 或 Python。

GitHub 仓库中源码位于 `multi-ip-proxy/`。根目录的 `.github/workflows/mip-proxy-release.yml` 在源码更新后自动构建、运行验证，通过后按程序版本创建 Release；已存在的版本不会覆盖，修改后发布需提升程序版本。

在 Linux 开发机安装 Go、Python 3、iproute2、procps、curl 后，可运行完整验证（需要 root 或 sudo 创建隔离命名空间）：

```sh
python3 scripts/build.py
sh scripts/verify-linux.sh
```

验证脚本使用固定版本、固定 SHA256 的核心、Mihomo 和 Pebble，运行各协议转发、真实 HTTP-01/IP 证书签发和四种 TLS 协议客户端转发、完全禁用 IPv6 后的转发、策略路由、IPv6 禁用与恢复，以及自动安装/升级/校验失败测试。Pebble 仅作测试 CA，测试根证书只放入临时进程配置。

离线生成 8 份**独立的单协议**示例，并用固定版本核心检查各份服务端及客户端配置：

```sh
./mip-proxy core --out ./sing-box
./mip-proxy demo --protocol all --out ./demo
./sing-box check -c ./demo/reality/server.json
./sing-box check -c ./demo/reality/export/sing-box.json
```

`all` 只用于生成互相隔离的测试示例，真实状态和向导不接受多协议。

真实核心集成测试启动 2 个独立 IP 节点，分别验证 TCP 与 64/1200 字节 UDP 数据转发及实际源地址。设置 `MIP_MIHOMO` 时，也会使用 Mihomo 验证各节点的 TCP/UDP 转发。测试证书信任仅存在于测试配置中。

```sh
MIP_CORE=/absolute/path/to/sing-box go test -v ./integration
```

覆盖全部 8 类（含 WireGuard）时必须在独立 Linux 网络命名空间中执行。WireGuard 测试会调整该命名空间的本地路由优先级，因此脚本与测试都会检查隔离条件：

```sh
go test -c -o /tmp/mip-core.test ./integration
sudo env MIP_CORE=/absolute/path/to/sing-box MIP_MIHOMO=/absolute/path/to/mihomo \
  unshare -n sh integration/run-isolated.sh /tmp/mip-core.test -test.v -test.timeout=120s
```

Linux 路由测试必须在全新网络命名空间中执行，验证重复应用、精确删除、多出口选路与保留无关路由表：

```sh
go test -c -o /tmp/mip-network.test ./internal/network
sudo env MIP_ROUTE_INTEGRATION=1 unshare -n /tmp/mip-network.test -test.run TestLinuxRoutesIntegration -test.v
```

实际跨公网连通性、云平台 NAT、安全组及受限网络中的抗封锁表现，仍需在目标服务器与外部网络验收。

开发验证在 Debian 13 的隔离网络命名空间完成。amd64 版本运行过真实核心和客户端转发测试；arm64 为交叉编译产物，尚未在 arm64 机器上实测。当前测试环境未运行 systemd，部署事务与服务失败回滚通过模拟测试验证，正式服务器上的服务生命周期仍需验收。
