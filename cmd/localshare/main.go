// 局域网共享的启动程序。命令行直接运行，或者打包成 Mac 的 .app 双击运行。
package main

import (
	"crypto/rand"
	"encoding/json"
	"flag"
	"fmt"
	"math/big"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"time"

	qrcode "github.com/skip2/go-qrcode"

	"localshare/core"
)

type config struct {
	Password string `json:"password"`
}

func main() {
	home, _ := os.UserHomeDir()
	port := flag.Int("port", 8000, "端口，被占用时自动往后找")
	dir := flag.String("dir", filepath.Join(home, "Localshare"), "数据目录（共享的文字和文件存这里）")
	password := flag.String("password", "", "指定访问码（会记住，下次沿用）")
	noPassword := flag.Bool("no-password", false, "这次不设访问码，谁连上 Wi-Fi 都能进")
	noOpen := flag.Bool("no-open", false, "启动后不自动打开浏览器")

	// 从 .app 里双击启动时，系统可能塞进一些奇怪的参数，直接忽略，全用默认值。
	if !inAppBundle() {
		flag.Parse()
	}

	// 已经开着了就别再开一个，直接把页面打开。
	if running(*port) {
		local := fmt.Sprintf("http://127.0.0.1:%d", *port)
		fmt.Println("局域网共享已经在运行：" + local)
		if !*noOpen {
			openBrowser(local)
		}
		return
	}

	pw := resolvePassword(*dir, *password, *noPassword)
	s := core.New(*dir, *port, pw)
	done := make(chan struct{})
	s.OnStop = func() { close(done) }
	if err := s.Start(); err != nil {
		fmt.Fprintln(os.Stderr, "启动失败：", err)
		os.Exit(1)
	}

	printBanner(s, *dir)
	if !*noOpen {
		openBrowser(s.LocalURL())
	}

	sig := make(chan os.Signal, 1)
	signal.Notify(sig, os.Interrupt, syscall.SIGTERM)
	select {
	case <-sig:
		s.Stop()
	case <-done:
	}
	fmt.Println("\n  共享已停止")
}

func inAppBundle() bool {
	exe, _ := os.Executable()
	return strings.Contains(exe, ".app/Contents/MacOS/")
}

func running(port int) bool {
	c := http.Client{Timeout: 800 * time.Millisecond}
	r, err := c.Get(fmt.Sprintf("http://127.0.0.1:%d/api/ping", port))
	if err != nil {
		return false
	}
	defer r.Body.Close()
	var v struct{ App string }
	json.NewDecoder(r.Body).Decode(&v)
	return v.App == core.AppID
}

// resolvePassword：命令行指定 > 上次记住的 > 随机生成 4 位数字。
func resolvePassword(dir, flagPw string, none bool) string {
	if none {
		return ""
	}
	path := filepath.Join(dir, "config.json")
	var cfg config
	if b, err := os.ReadFile(path); err == nil {
		json.Unmarshal(b, &cfg)
	}
	switch {
	case flagPw != "":
		cfg.Password = flagPw
	case cfg.Password == "":
		n, _ := rand.Int(rand.Reader, big.NewInt(10000))
		cfg.Password = fmt.Sprintf("%04d", n.Int64())
	default:
		return cfg.Password
	}
	os.MkdirAll(dir, 0o755)
	b, _ := json.MarshalIndent(cfg, "", "  ")
	os.WriteFile(path, b, 0o600)
	return cfg.Password
}

func printBanner(s *core.Server, dir string) {
	fmt.Println()
	fmt.Println("  局域网共享已启动")
	fmt.Println("    本机打开:   " + s.LocalURL())
	fmt.Println("    其他设备:   " + s.URL())
	if s.Password != "" {
		fmt.Println("    访问码:     " + s.Password)
	}
	fmt.Println("    数据目录:   " + dir)
	if q, err := qrcode.New(s.JoinURL(), qrcode.Low); err == nil {
		fmt.Println("\n  手机扫码直接进入：")
		fmt.Print(q.ToSmallString(false))
	}
	fmt.Println("  按 Ctrl+C 停止")
}

func openBrowser(url string) {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("open", url)
	case "windows":
		cmd = exec.Command("rundll32", "url.dll,FileProtocolHandler", url)
	default:
		cmd = exec.Command("xdg-open", url)
	}
	cmd.Start()
}
