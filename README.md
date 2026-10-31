# singboxtoips

## Debian 多 IP 单协议代理节点工具

交互选择一种协议，为服务器的多个 IPv4 地址生成独立节点。支持 SOCKS5、Shadowsocks、VMess、VLESS + REALITY、Trojan、Hysteria2、TUIC、WireGuard。

支持 Debian 12+、systemd、amd64/arm64。默认只选择协议，自动使用全部 IP、推荐路由与 443 端口；TLS 协议可自动申请和续期公网 IP 证书，无需域名。正式部署禁用整台服务器的 IPv6，节点仅使用 IPv4；支持配置预览、节点导出、检测和部署回滚。

```sh
curl -fL https://github.com/qq1527949891/singboxtoips/releases/latest/download/install.sh -o install.sh
sudo sh install.sh
```

安装入口自动识别架构，按需下载对应 Release 安装包并校验 SHA256。已安装相同版本时复用，解压安装包时优先使用包内程序。每次部署只选择一种协议，全部 IP 节点统一使用该协议。

v0.1.6 加强长期运行稳定性：服务与依赖故障自动恢复，限制辅助 HTTP 的连接与超时，缓存订阅和续期证书。加入真实 systemd 故障注入、并发竞态检测和全部协议的反复 TCP/UDP 会话验证。升级后需在向导执行“创建/更新节点”以应用新服务配置。继续使用公开仓库下载，无需 GitHub 令牌。

- [完整说明与源码](multi-ip-proxy/README.md)
- [发布包](https://github.com/qq1527949891/singboxtoips/releases)
- [更新记录](multi-ip-proxy/RELEASE_NOTES.txt)

原有 Shell 脚本保留在仓库根目录。

自动证书需要验证 IP 的 TCP 80 允许公网访问。自定义 IP、端口和证书可运行 `mip-proxy wizard --advanced`。

部署成功后直接在控制台打印全部节点：SOCKS5 使用 `IP/端口/用户名/密码`，Shadowsocks 使用 `IP/端口/aes-256-gcm/密码`，其他协议使用标准分享链接。WireGuard 使用 Mihomo 单行节点格式，每个 IP 一行，同时保留原生 `.conf`。

Trojan、Hysteria2、TUIC、WireGuard 部署后同时打印带随机令牌的 HTTPS 订阅地址，提供 Mihomo、sing-box 配置和对应协议的导入格式。订阅端口自动选择，需放行输出的 TCP 端口；WireGuard 的订阅证书也可直接使用公网 IP 自动申请。使用 `mip-proxy subscriptions` 可再次查看地址。
