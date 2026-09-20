# 给 AI 助手的说明

这个仓库是「局域网共享」：在一台电脑上启动后，同一个 Wi-Fi 下的手机和电脑打开网页就能共享文字和文件。

## 用户说「帮我启动」「开一下共享」之类的话时

1. 在仓库根目录后台启动，并把输出写到日志里（这个服务要一直运行，不能放在前台等它结束）：

   ```bash
   mkdir -p ~/Localshare && nohup ./scripts/start.sh > ~/Localshare/localshare.log 2>&1 &
   ```

   Windows 上没有 `start.sh`：有 Go 就执行 `go run ./cmd/localshare`（放后台运行）；没有 Go 就从本仓库的 GitHub Releases 下载 `localshare-windows-amd64.exe` 来运行。

2. 等两三秒后读日志 `cat ~/Localshare/localshare.log`，把里面的「其他设备」地址和「访问码」告诉用户。
   程序会自动在用户的浏览器里打开页面，页面上有二维码，别人用手机扫码就能直接进入。
   如果日志里显示「已经在运行」，说明服务本来就开着，直接告诉用户打开 http://127.0.0.1:8000 查看地址和二维码。

3. 提醒用户：如果 Mac 弹窗问是否允许传入连接，点「允许」，不然其他设备连不上。

## 常用参数（跟在 `./scripts/start.sh` 后面）

- `-password 1234`：指定访问码（会被记住，下次沿用）
- `-no-password`：这次不设访问码
- `-port 9000`：换端口（默认 8000，被占用时会自动往后找）
- `-dir 路径`：换数据目录（默认 `~/Localshare`）
- `-no-open`：不自动打开浏览器

## 停止

```bash
curl -X POST http://127.0.0.1:8000/api/stop
```

或者让用户在本机页面上点「停止共享」。

## 代码结构

- `core/`：服务端（Go），页面在 `core/web/`，编译时会嵌进程序里
- `cmd/localshare/`：启动程序（命令行 / Mac App 共用）
- `scripts/build-mac.sh`：打包 Mac 客户端到 `dist/`
- `.github/workflows/release.yml`：推送 `v*` 标签时自动编译各平台程序并发布到 Releases
