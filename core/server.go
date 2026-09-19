// Package core 是局域网共享的服务端：共享文字、共享文件、访问码、邀请二维码。
// 命令行、Mac 客户端（以及以后的安卓客户端）都复用这一份代码。
package core

import (
	"context"
	"crypto/subtle"
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"
	"time"

	qrcode "github.com/skip2/go-qrcode"
)

//go:embed web
var webFS embed.FS

const (
	AppID      = "lanshare"
	cookieName = "lanshare_key"
)

type Server struct {
	Dir      string // 数据目录：text.txt 和 files/ 都放这里
	Password string // 访问码，空字符串表示不需要
	Port     int    // 首选端口，被占用时自动往后找

	OnStop func() // 网页上点「停止共享」时调用

	textMu sync.Mutex
	srv    *http.Server
	port   int
}

func New(dir string, port int, password string) *Server {
	return &Server{Dir: dir, Port: port, Password: password}
}

func (s *Server) filesDir() string { return filepath.Join(s.Dir, "files") }
func (s *Server) textPath() string { return filepath.Join(s.Dir, "text.txt") }

// Start 开始监听（不阻塞）。首选端口被占用时依次尝试后面 20 个端口。
func (s *Server) Start() error {
	if err := os.MkdirAll(s.filesDir(), 0o755); err != nil {
		return err
	}
	var ln net.Listener
	var err error
	for p := s.Port; p < s.Port+20; p++ {
		if ln, err = net.Listen("tcp", fmt.Sprintf(":%d", p)); err == nil {
			s.port = p
			break
		}
	}
	if ln == nil {
		return fmt.Errorf("找不到可用端口: %w", err)
	}
	s.srv = &http.Server{Handler: s.routes()}
	go s.srv.Serve(ln)
	return nil
}

func (s *Server) Stop() {
	if s.srv != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		s.srv.Shutdown(ctx)
	}
}

func (s *Server) ActualPort() int { return s.port }

// URL 是局域网里其他设备访问的地址。每次调用都重新取 IP，换了 Wi-Fi 也能跟上。
func (s *Server) URL() string { return fmt.Sprintf("http://%s:%d", LanIP(), s.port) }

// JoinURL 带上访问码，扫码即可直接进入。
func (s *Server) JoinURL() string {
	if s.Password == "" {
		return s.URL() + "/"
	}
	return s.URL() + "/?key=" + url.QueryEscape(s.Password)
}

func (s *Server) LocalURL() string { return fmt.Sprintf("http://127.0.0.1:%d", s.port) }

// ---------- 路由与鉴权 ----------

func (s *Server) routes() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/ping", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, 200, map[string]any{"app": AppID})
	})
	mux.HandleFunc("POST /api/login", s.handleLogin)
	mux.HandleFunc("GET /{$}", s.auth(s.handleIndex, true))
	mux.HandleFunc("GET /api/info", s.auth(s.handleInfo, false))
	mux.HandleFunc("GET /api/qr", s.auth(s.handleQR, false))
	mux.HandleFunc("GET /api/text", s.auth(s.handleGetText, false))
	mux.HandleFunc("POST /api/text", s.auth(s.handleSetText, false))
	mux.HandleFunc("GET /api/files", s.auth(s.handleListFiles, false))
	mux.HandleFunc("POST /api/upload", s.auth(s.handleUpload, false))
	mux.HandleFunc("POST /api/delete", s.auth(s.handleDelete, false))
	mux.HandleFunc("GET /files/{name}", s.auth(s.handleDownload, false))
	mux.HandleFunc("POST /api/stop", s.handleStop)
	return mux
}

func isLocal(r *http.Request) bool {
	host, _, _ := net.SplitHostPort(r.RemoteAddr)
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

func (s *Server) keyOK(k string) bool {
	return subtle.ConstantTimeCompare([]byte(k), []byte(s.Password)) == 1
}

func (s *Server) setCookie(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{Name: cookieName, Value: s.Password, Path: "/",
		MaxAge: 30 * 24 * 3600, HttpOnly: true, SameSite: http.SameSiteLaxMode})
}

// auth：本机访问免验证；否则需要 cookie 里的访问码，或者 URL 里带 ?key=（扫码进入）。
func (s *Server) auth(h http.HandlerFunc, page bool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if s.Password == "" || isLocal(r) {
			h(w, r)
			return
		}
		if c, err := r.Cookie(cookieName); err == nil && s.keyOK(c.Value) {
			h(w, r)
			return
		}
		if page {
			if k := r.URL.Query().Get("key"); k != "" && s.keyOK(k) {
				s.setCookie(w)
				http.Redirect(w, r, "/", http.StatusFound)
				return
			}
			serveEmbedded(w, "web/login.html")
			return
		}
		writeJSON(w, 401, map[string]any{"error": "需要访问码"})
	}
}

func (s *Server) handleLogin(w http.ResponseWriter, r *http.Request) {
	var req struct{ Password string }
	json.NewDecoder(io.LimitReader(r.Body, 4096)).Decode(&req)
	if s.Password != "" && !s.keyOK(strings.TrimSpace(req.Password)) {
		time.Sleep(500 * time.Millisecond) // 简单拖慢暴力猜码
		writeJSON(w, 403, map[string]any{"error": "访问码不对"})
		return
	}
	s.setCookie(w)
	writeJSON(w, 200, map[string]any{"ok": true})
}

// ---------- 页面与信息 ----------

func serveEmbedded(w http.ResponseWriter, name string) {
	b, _ := webFS.ReadFile(name)
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.Write(b)
}

func (s *Server) handleIndex(w http.ResponseWriter, r *http.Request) {
	serveEmbedded(w, "web/index.html")
}

func (s *Server) handleInfo(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, 200, map[string]any{
		"url":      s.URL(),
		"joinUrl":  s.JoinURL(),
		"password": s.Password,
		"isHost":   isLocal(r),
		"host":     hostname(),
	})
}

func (s *Server) handleQR(w http.ResponseWriter, r *http.Request) {
	png, err := qrcode.Encode(s.JoinURL(), qrcode.Medium, 320)
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	w.Header().Set("Content-Type", "image/png")
	w.Header().Set("Cache-Control", "no-store")
	w.Write(png)
}

func (s *Server) handleStop(w http.ResponseWriter, r *http.Request) {
	if !isLocal(r) {
		writeJSON(w, 403, map[string]any{"error": "只能在开启共享的电脑上停止"})
		return
	}
	writeJSON(w, 200, map[string]any{"ok": true})
	go func() {
		time.Sleep(300 * time.Millisecond)
		s.Stop()
		if s.OnStop != nil {
			s.OnStop()
		}
	}()
}

// ---------- 文字 ----------

func (s *Server) textVersion() int64 {
	if st, err := os.Stat(s.textPath()); err == nil {
		return st.ModTime().UnixNano()
	}
	return 0
}

func (s *Server) handleGetText(w http.ResponseWriter, r *http.Request) {
	s.textMu.Lock()
	defer s.textMu.Unlock()
	b, _ := os.ReadFile(s.textPath())
	writeJSON(w, 200, map[string]any{"text": string(b), "version": s.textVersion()})
}

func (s *Server) handleSetText(w http.ResponseWriter, r *http.Request) {
	var req struct{ Text string }
	if err := json.NewDecoder(io.LimitReader(r.Body, 10<<20)).Decode(&req); err != nil {
		writeJSON(w, 400, map[string]any{"error": "格式不对"})
		return
	}
	s.textMu.Lock()
	defer s.textMu.Unlock()
	if err := os.WriteFile(s.textPath(), []byte(req.Text), 0o644); err != nil {
		writeJSON(w, 500, map[string]any{"error": err.Error()})
		return
	}
	writeJSON(w, 200, map[string]any{"ok": true, "version": s.textVersion()})
}

// ---------- 文件 ----------

// safeName 只保留文件名本身，挡掉 ../ 之类的路径穿越；以 . 开头的是内部临时文件，也不允许。
func safeName(name string) (string, bool) {
	name = strings.TrimSpace(filepath.Base(strings.ReplaceAll(name, "\\", "/")))
	if name == "" || name == "." || name == ".." || name == "/" || strings.HasPrefix(name, ".") {
		return "", false
	}
	return name, true
}

func (s *Server) handleListFiles(w http.ResponseWriter, r *http.Request) {
	entries, _ := os.ReadDir(s.filesDir())
	type item struct {
		Name  string `json:"name"`
		Size  int64  `json:"size"`
		Mtime int64  `json:"mtime"`
	}
	items := []item{}
	for _, e := range entries {
		if e.IsDir() || strings.HasPrefix(e.Name(), ".") {
			continue
		}
		if info, err := e.Info(); err == nil {
			items = append(items, item{e.Name(), info.Size(), info.ModTime().UnixMilli()})
		}
	}
	sort.Slice(items, func(i, j int) bool { return items[i].Mtime > items[j].Mtime })
	writeJSON(w, 200, items)
}

// uniquePath 同名文件自动改名为 xxx(1).ext，不覆盖已有文件。
func (s *Server) uniquePath(name string) string {
	ext := filepath.Ext(name)
	root := strings.TrimSuffix(name, ext)
	p := filepath.Join(s.filesDir(), name)
	for i := 1; ; i++ {
		if _, err := os.Stat(p); errors.Is(err, os.ErrNotExist) {
			return p
		}
		p = filepath.Join(s.filesDir(), fmt.Sprintf("%s(%d)%s", root, i, ext))
	}
}

func (s *Server) handleUpload(w http.ResponseWriter, r *http.Request) {
	name, ok := safeName(r.URL.Query().Get("name"))
	if !ok {
		writeJSON(w, 400, map[string]any{"error": "文件名无效"})
		return
	}
	// 先写到隐藏的临时文件，传完再改名，别人不会看到传了一半的文件。
	tmp, err := os.CreateTemp(s.filesDir(), ".uploading-*")
	if err != nil {
		writeJSON(w, 500, map[string]any{"error": err.Error()})
		return
	}
	_, err = io.Copy(tmp, r.Body)
	tmp.Close()
	if err != nil {
		os.Remove(tmp.Name())
		writeJSON(w, 500, map[string]any{"error": "上传中断: " + err.Error()})
		return
	}
	s.textMu.Lock() // 借用这把锁，避免两个同名文件同时改名撞车
	final := s.uniquePath(name)
	err = os.Rename(tmp.Name(), final)
	s.textMu.Unlock()
	if err != nil {
		os.Remove(tmp.Name())
		writeJSON(w, 500, map[string]any{"error": err.Error()})
		return
	}
	writeJSON(w, 200, map[string]any{"ok": true, "name": filepath.Base(final)})
}

func (s *Server) handleDelete(w http.ResponseWriter, r *http.Request) {
	var req struct{ Name string }
	json.NewDecoder(io.LimitReader(r.Body, 4096)).Decode(&req)
	if name, ok := safeName(req.Name); ok {
		os.Remove(filepath.Join(s.filesDir(), name))
	}
	writeJSON(w, 200, map[string]any{"ok": true})
}

func (s *Server) handleDownload(w http.ResponseWriter, r *http.Request) {
	name, ok := safeName(r.PathValue("name"))
	if !ok {
		http.NotFound(w, r)
		return
	}
	f, err := os.Open(filepath.Join(s.filesDir(), name))
	if err != nil {
		http.NotFound(w, r)
		return
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil || st.IsDir() {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Disposition", "attachment; filename*=UTF-8''"+url.PathEscape(name))
	http.ServeContent(w, r, name, st.ModTime(), f)
}

// ---------- 工具函数 ----------

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(code)
	json.NewEncoder(w).Encode(v)
}

// hostname 给访客看的电脑名。Mac 上 os.Hostname 经常是 IP，优先用「电脑名称」。
var hostname = sync.OnceValue(func() string {
	if runtime.GOOS == "darwin" {
		if b, err := exec.Command("scutil", "--get", "ComputerName").Output(); err == nil {
			if n := strings.TrimSpace(string(b)); n != "" {
				return n
			}
		}
	}
	h, _ := os.Hostname()
	return strings.TrimSuffix(h, ".local")
})

// LanIP 找本机在局域网里的 IPv4 地址。
func LanIP() string {
	// 先看系统默认走哪块网卡（UDP 不会真的发包）
	if c, err := net.Dial("udp", "10.255.255.255:1"); err == nil {
		ip := c.LocalAddr().(*net.UDPAddr).IP
		c.Close()
		if ip.IsPrivate() {
			return ip.String()
		}
	}
	// 没有默认路由（比如 Wi-Fi 没接外网）时，挑第一个私有地址
	addrs, _ := net.InterfaceAddrs()
	for _, a := range addrs {
		if n, ok := a.(*net.IPNet); ok && n.IP.To4() != nil && n.IP.IsPrivate() {
			return n.IP.String()
		}
	}
	return "127.0.0.1"
}
