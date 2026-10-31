# singboxtoips

## Debian 多 IP 单协议代理节点工具

交互选择一种协议，为服务器的多个 IPv4 地址生成独立节点。支持 SOCKS5、Shadowsocks、VMess、VLESS + REALITY、Trojan、Hysteria2、TUIC、WireGuard。

支持 Debian 12+、systemd、amd64/arm64。正式部署自动禁用整台服务器的 IPv6，节点仅使用 IPv4；支持配置预览、节点导出、检测和部署回滚。

```sh
curl -fL https://github.com/qq1527949891/singboxtoips/releases/latest/download/install.sh -o install.sh
sudo sh install.sh
```

安装入口自动识别架构，按需下载对应 Release 安装包并校验 SHA256。已安装相同版本时复用，解压安装包时优先使用包内程序。每次部署只选择一种协议，全部 IP 节点统一使用该协议。

- [完整说明与源码](multi-ip-proxy/README.md)
- [发布包](https://github.com/qq1527949891/singboxtoips/releases)
- [更新记录](multi-ip-proxy/RELEASE_NOTES.txt)

原有 Shell 脚本保留在仓库根目录。
