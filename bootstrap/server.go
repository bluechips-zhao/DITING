package bootstrap

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/Autumn-27/artex/config"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

type CheckDatabase func(context.Context, string) error

// DatabaseCheck connects without applying migrations or creating a database.
func DatabaseCheck(ctx context.Context, dsn string) error {
	ctx, cancel := context.WithTimeout(ctx, 8*time.Second)
	defer cancel()
	cfg, err := pgx.ParseConfig(dsn)
	if err != nil {
		return errors.New("数据库连接配置格式无效")
	}
	cfg.ConnectTimeout = 8 * time.Second
	conn, err := pgx.ConnectConfig(ctx, cfg)
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) {
			switch pgErr.Code {
			case "28P01", "28000":
				return errors.New("数据库认证失败，请检查用户名、密码及 PostgreSQL 访问规则")
			case "3D000":
				return errors.New("数据库不存在，请先在 PostgreSQL 创建指定数据库")
			}
		}
		return errors.New("无法连接数据库，请检查主机、端口、SSL、数据库服务和访问规则")
	}
	defer conn.Close(context.Background())
	var version int
	if err = conn.QueryRow(ctx, "SELECT current_setting('server_version_num')::int").Scan(&version); err != nil {
		return errors.New("无法查询数据库版本，请检查访问权限")
	}
	if version < 160000 {
		return errors.New("请使用 PostgreSQL 16 或更新版本")
	}
	return nil
}

type Options struct {
	ConfigPath, DataDir, KeyDir string
	ExistingDSN                 string
	EnvironmentDSN              bool
	InitialError                string
	Check                       CheckDatabase
}

type Wizard struct {
	opts     Options
	token    string
	mu       sync.Mutex
	done     chan struct{}
	finished bool
}

func New(opts Options) (*Wizard, error) {
	seed := make([]byte, 32)
	if _, err := rand.Read(seed); err != nil {
		return nil, err
	}
	if opts.Check == nil {
		opts.Check = DatabaseCheck
	}
	return &Wizard{opts: opts, token: hex.EncodeToString(seed), done: make(chan struct{})}, nil
}

func localRequest(r *http.Request) bool {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return false
	}
	ip := net.ParseIP(host)
	if ip == nil || !ip.IsLoopback() {
		return false
	}
	host = r.Host
	if h, _, err := net.SplitHostPort(host); err == nil {
		host = h
	}
	host = strings.Trim(host, "[]")
	ip = net.ParseIP(host)
	return strings.EqualFold(host, "localhost") || (ip != nil && ip.IsLoopback())
}

func (w *Wizard) Handler() http.Handler {
	return http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) {
		rw.Header().Set("Cache-Control", "no-store")
		rw.Header().Set("X-Content-Type-Options", "nosniff")
		rw.Header().Set("Referrer-Policy", "no-referrer")
		rw.Header().Set("Content-Security-Policy", "default-src 'none'; style-src 'unsafe-inline'; script-src 'unsafe-inline'; connect-src 'self'; frame-ancestors 'none'; base-uri 'none'; form-action 'self'")
		if !localRequest(r) {
			http.Error(rw, "Local setup only", http.StatusForbidden)
			return
		}
		if origin := r.Header.Get("Origin"); origin != "" {
			u, err := url.Parse(origin)
			if err != nil || u.Scheme != "http" || u.Host != r.Host {
				http.Error(rw, "Origin rejected", http.StatusForbidden)
				return
			}
		}
		if r.Method == "GET" && (r.URL.Path == "/" || r.URL.Path == "/setup" || r.URL.Path == "/setup/") {
			rw.Header().Set("Content-Type", "text/html; charset=utf-8")
			fmt.Fprint(rw, strings.ReplaceAll(page, "SETUP_TOKEN", w.token))
			return
		}
		if subtle.ConstantTimeCompare([]byte(r.Header.Get("X-Diting-Setup")), []byte(w.token)) != 1 {
			http.Error(rw, "Setup token required", http.StatusForbidden)
			return
		}
		if r.Method == "GET" && r.URL.Path == "/api/bootstrap/status" {
			respond(rw, 200, map[string]any{"environment": Inspect(w.opts.DataDir, w.opts.KeyDir), "environment_dsn": w.opts.EnvironmentDSN, "error": w.opts.InitialError})
			return
		}
		if r.Method != "POST" || (r.URL.Path != "/api/bootstrap/check" && r.URL.Path != "/api/bootstrap/save" && r.URL.Path != "/api/bootstrap/retry") {
			http.NotFound(rw, r)
			return
		}
		w.mu.Lock()
		defer w.mu.Unlock()
		if w.finished {
			respond(rw, 409, map[string]string{"error": "配置已保存，正在启动应用"})
			return
		}
		retry := r.URL.Path == "/api/bootstrap/retry"
		if w.opts.EnvironmentDSN && !retry {
			respond(rw, 409, map[string]string{"error": "ARTEX_PG_DSN 优先于文件。请修正环境变量或启动数据库后重试连接"})
			return
		}
		dsn := w.opts.ExistingDSN
		var database config.Database
		if !retry {
			r.Body = http.MaxBytesReader(rw, r.Body, 16384)
			decoder := json.NewDecoder(r.Body)
			decoder.DisallowUnknownFields()
			if err := decoder.Decode(&database); err != nil {
				respond(rw, 400, map[string]string{"error": "连接参数无效"})
				return
			}
			if err := decoder.Decode(new(any)); err != io.EOF {
				respond(rw, 400, map[string]string{"error": "连接参数必须是单个 JSON 对象"})
				return
			}
			var err error
			dsn, err = database.ConnectionString()
			if err != nil {
				respond(rw, 400, map[string]string{"error": err.Error()})
				return
			}
		}
		if dsn == "" {
			respond(rw, 400, map[string]string{"error": "尚未配置数据库"})
			return
		}
		if err := w.opts.Check(r.Context(), dsn); err != nil {
			respond(rw, 422, map[string]string{"error": err.Error()})
			return
		}
		if r.URL.Path == "/api/bootstrap/check" {
			respond(rw, 200, map[string]bool{"ok": true})
			return
		}
		if !retry {
			if err := SaveDatabase(w.opts.ConfigPath, database); err != nil {
				respond(rw, 500, map[string]string{"error": err.Error()})
				return
			}
		}
		w.finished = true
		respond(rw, 200, map[string]bool{"ok": true})
		close(w.done)
	})
}

func respond(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(value)
}

// EnsureDatabase temporarily binds a loopback-only wizard when connection is absent
// or unavailable. Successful save/retry returns to normal startup in the same process.
func EnsureDatabase(ctx context.Context, addr string, opts Options) error {
	if opts.Check == nil {
		opts.Check = DatabaseCheck
	}
	if opts.ExistingDSN != "" {
		if err := opts.Check(ctx, opts.ExistingDSN); err == nil {
			return nil
		} else {
			opts.InitialError = err.Error()
		}
	}
	if ctx.Err() != nil {
		return ctx.Err()
	}
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		return fmt.Errorf("setup listen address: %w", err)
	}
	if host == "" || host == "0.0.0.0" || host == "::" {
		host = "127.0.0.1"
	}
	if !strings.EqualFold(host, "localhost") {
		ip := net.ParseIP(host)
		if ip == nil || !ip.IsLoopback() {
			return errors.New("数据库未就绪；请使用本机监听地址打开初始化向导，或先配置数据库")
		}
	}
	listener, err := net.Listen("tcp", net.JoinHostPort(host, port))
	if err != nil {
		return err
	}
	w, err := New(opts)
	if err != nil {
		listener.Close()
		return err
	}
	srv := &http.Server{Handler: w.Handler(), ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 15 * time.Second, WriteTimeout: 20 * time.Second, IdleTimeout: 30 * time.Second}
	result := make(chan error, 1)
	go func() { result <- srv.Serve(listener) }()
	log.Printf("[setup] 请在本机浏览器打开 http://%s/ 完成数据库初始化", listener.Addr())
	// A configured database can be temporarily unavailable when the host or
	// container engine restarts. Keep the local repair wizard available, but
	// resume automatically once that same database is reachable again.
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()
wait:
	for {
		select {
		case <-w.done:
			err = nil
			break wait
		case <-ctx.Done():
			err = ctx.Err()
			break wait
		case serveErr := <-result:
			err = serveErr
			break wait
		case <-ticker.C:
			if opts.ExistingDSN == "" || ctx.Err() != nil {
				continue
			}
			w.mu.Lock()
			if !w.finished && opts.Check(ctx, opts.ExistingDSN) == nil && ctx.Err() == nil {
				w.finished = true
				close(w.done)
			}
			w.mu.Unlock()
		}
	}
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	srv.Shutdown(shutdownCtx)
	srv.Close()
	return err
}
