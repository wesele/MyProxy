package main

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"fmt"
	"io"
	"math/big"
	"net"
	"net/http"
	"os"
	"os/signal"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/user/qwenportal/internal/api"
	"github.com/user/qwenportal/internal/auth"
	"github.com/user/qwenportal/internal/config"
	"github.com/user/qwenportal/internal/db"
	"github.com/user/qwenportal/internal/middleware"
	"github.com/user/qwenportal/internal/proxy"
	"go.uber.org/zap"
)

func main() {
	cfg, err := config.Load("config.yaml")
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to load config: %v\n", err)
		os.Exit(1)
	}

	logger, _ := zap.NewProduction()
	defer logger.Sync()

	store, err := db.New(cfg.Database.Path)
	if err != nil {
		logger.Fatal("failed to init database", zap.Error(err))
	}
	defer store.Close()

	bootstrapAdminKey(logger, store)

	router_ := proxy.NewRouter(store)
	if err := router_.Refresh(); err != nil {
		logger.Warn("failed to refresh router", zap.Error(err))
	}

	forwarder := proxy.NewForwarder(logger)

	openAIHandler := api.NewOpenAIHandler(forwarder, router_, logger, store)
	geminiHandler := api.NewGeminiHandler(forwarder, router_, logger)
	openAIHandler.SetGeminiHandler(geminiHandler)
	responsesHandler := api.NewResponsesHandler(forwarder, router_, logger, store)
	responsesHandler.SetGeminiHandler(geminiHandler)
	claudeHandler := api.NewClaudeHandler(forwarder, router_, logger)
	adminHandler := api.NewAdminHandler(logger, router_, store)

	authManager := auth.NewAuthManager("data/password.txt")
	loginHandler := api.NewLoginHandler(authManager, logger)

	if cfg.Server.IsTLSEnabled() {
		ensureCert(cfg.Server.CertFile, cfg.Server.KeyFile, logger)
	}

	gin.SetMode(gin.ReleaseMode)
	r := gin.New()
	r.Use(gin.Recovery())
	r.Use(middleware.CORS())
	r.Use(middleware.RequestLogger(logger, store))

	r.GET("/", func(c *gin.Context) {
		c.Redirect(http.StatusFound, "/admin/dashboard")
	})

	apiAuth := middleware.AuthMiddleware(store)
	r.GET("/v1/models", apiAuth, openAIHandler.ListModels)
	r.POST("/v1/chat/completions", apiAuth, openAIHandler.ChatCompletions)
	r.POST("/v1/embeddings", apiAuth, openAIHandler.Embeddings)
	r.POST("/v1/responses", apiAuth, responsesHandler.Responses)
	r.POST("/v1/messages", apiAuth, claudeHandler.Messages)

	loginRequired := middleware.LoginRequired(authManager)

	r.POST("/admin/api/login", loginHandler.Login)
	r.POST("/admin/api/logout", loginHandler.Logout)
	r.GET("/admin/api/session", loginHandler.CheckSession)

	adminAPI := r.Group("/admin/api", loginRequired, middleware.AdminAuth(store))
	{
		adminAPI.GET("/providers", adminHandler.ListProviders)
		adminAPI.POST("/providers", adminHandler.CreateProvider)
		adminAPI.GET("/providers/:id", adminHandler.GetProvider)
		adminAPI.PUT("/providers/:id", adminHandler.UpdateProvider)
		adminAPI.DELETE("/providers/:id", adminHandler.DeleteProvider)
		adminAPI.GET("/providers/export", adminHandler.ExportProviders)
		adminAPI.POST("/providers/import", adminHandler.ImportProviders)

		adminAPI.GET("/keys", adminHandler.ListApiKeys)
		adminAPI.POST("/keys", adminHandler.CreateApiKey)
		adminAPI.PUT("/keys/:id", adminHandler.UpdateApiKey)
		adminAPI.DELETE("/keys/:id", adminHandler.DeleteApiKey)

		adminAPI.GET("/stats", adminHandler.GetStats)
		adminAPI.GET("/logs", adminHandler.GetModelLogs)
		adminAPI.POST("/providers/fetch-models", adminHandler.FetchProviderModels)
		adminAPI.POST("/providers/test", adminHandler.TestProvider)
		adminAPI.POST("/models/test", adminHandler.TestModels)
		adminAPI.POST("/training/start", adminHandler.TrainingStart)
		adminAPI.POST("/training/stop", adminHandler.TrainingStop)
		adminAPI.GET("/training/stats", adminHandler.TrainingStats)
		adminAPI.GET("/training/active", adminHandler.TrainingActive)
	}

	var flaskCmd *exec.Cmd
	if cfg.WebUI.Enabled {
		flaskCmd = startFlask(cfg, logger)
	}

	r.NoRoute(func(c *gin.Context) {
		p := c.Request.URL.Path
		if strings.HasPrefix(p, "/admin") {
			if p != "/admin/login" && !strings.HasPrefix(p, "/admin/static/") {
				sid, err := c.Cookie("session")
				if err != nil || sid == "" || !authManager.ValidateSession(sid) {
					c.Redirect(http.StatusFound, "/admin/login")
					return
				}
			}
			proxyToFlask(c, cfg.WebUI.Port)
			return
		}
		c.JSON(http.StatusNotFound, gin.H{"error": "not found"})
	})

	addr := fmt.Sprintf("%s:%d", cfg.Server.Host, cfg.Server.Port)
	srv := &http.Server{
		Addr:    addr,
		Handler: r,
	}

	printStartupBanner(addr, cfg.WebUI.Port, cfg.Server.IsTLSEnabled())

	go func() {
		if cfg.Server.IsTLSEnabled() {
			logger.Info("server starting with TLS", zap.String("addr", addr))
			if err := srv.ListenAndServeTLS(cfg.Server.CertFile, cfg.Server.KeyFile); err != nil && err != http.ErrServerClosed {
				logger.Fatal("server error", zap.Error(err))
			}
		} else {
			logger.Info("server starting", zap.String("addr", addr))
			if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
				logger.Fatal("server error", zap.Error(err))
			}
		}
	}()

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	logger.Info("shutting down...")

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if flaskCmd != nil && flaskCmd.Process != nil {
		killProcess(flaskCmd)
	}

	srv.Shutdown(ctx)
}

func killProcess(cmd *exec.Cmd) {
	if cmd.Process == nil {
		return
	}
	cmd.Process.Signal(os.Interrupt)
	time.Sleep(500 * time.Millisecond)
	if cmd.Process != nil {
		cmd.Process.Kill()
	}
	cmd.Wait()
}

func ensureCert(certPath, keyPath string, logger *zap.Logger) {
	if _, err := os.Stat(certPath); err == nil {
		if _, err := os.Stat(keyPath); err == nil {
			return
		}
	}

	logger.Info("generating self-signed TLS certificate",
		zap.String("cert", certPath),
		zap.String("key", keyPath),
	)

	priv, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		logger.Fatal("failed to generate private key", zap.Error(err))
	}

	serialNumber, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
	if err != nil {
		logger.Fatal("failed to generate serial number", zap.Error(err))
	}

	template := &x509.Certificate{
		SerialNumber: serialNumber,
		Subject: pkix.Name{
			CommonName: "QwenPortal",
		},
		NotBefore:             time.Now(),
		NotAfter:              time.Now().Add(10 * 365 * 24 * time.Hour),
		KeyUsage:              x509.KeyUsageKeyEncipherment | x509.KeyUsageDigitalSignature,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true,
		IPAddresses:           []net.IP{net.ParseIP("127.0.0.1"), net.ParseIP("::1")},
		DNSNames:              []string{"localhost"},
	}

	certDER, err := x509.CreateCertificate(rand.Reader, template, template, &priv.PublicKey, priv)
	if err != nil {
		logger.Fatal("failed to create certificate", zap.Error(err))
	}

	os.MkdirAll(filepath.Dir(certPath), 0755)
	certOut, err := os.Create(certPath)
	if err != nil {
		logger.Fatal("failed to write certificate", zap.Error(err))
	}
	pem.Encode(certOut, &pem.Block{Type: "CERTIFICATE", Bytes: certDER})
	certOut.Close()

	os.MkdirAll(filepath.Dir(keyPath), 0755)
	keyOut, err := os.Create(keyPath)
	if err != nil {
		logger.Fatal("failed to write private key", zap.Error(err))
	}
	b, err := x509.MarshalECPrivateKey(priv)
	if err != nil {
		logger.Fatal("failed to marshal private key", zap.Error(err))
	}
	pem.Encode(keyOut, &pem.Block{Type: "EC PRIVATE KEY", Bytes: b})
	keyOut.Close()
}

func startFlask(cfg *config.Config, logger *zap.Logger) *exec.Cmd {
	webUIPort := cfg.WebUI.Port
	if webUIPort == 0 {
		webUIPort = findAvailablePort()
	}

	webUIDir := findWebUIDir()

	scheme := "http"
	if cfg.Server.IsTLSEnabled() {
		scheme = "https"
	}
	apiBase := fmt.Sprintf("%s://127.0.0.1:%d/admin/api", scheme, cfg.Server.Port)

	cmd := exec.Command(cfg.WebUI.Python, "app.py")
	cmd.Dir = webUIDir
	cmd.Env = append(os.Environ(),
		"FLASK_PORT="+strconv.Itoa(webUIPort),
		"FLASK_DEBUG=0",
		"API_BASE="+apiBase,
		"GO_PORT="+strconv.Itoa(cfg.Server.Port),
	)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	if err := cmd.Start(); err != nil {
		logger.Warn("failed to start flask web ui", zap.Error(err))
		return nil
	}

	logger.Info("flask web ui started",
		zap.Int("port", webUIPort),
		zap.String("dir", webUIDir),
	)

	cfg.WebUI.Port = webUIPort
	return cmd
}

func bootstrapAdminKey(logger *zap.Logger, store db.Store) {
	keys, err := store.ListApiKeys()
	if err != nil || len(keys) > 0 {
		return
	}

	key, err := store.CreateApiKey("admin", 0)
	if err != nil {
		logger.Warn("failed to create admin key", zap.Error(err))
		return
	}

	const sep = "============================================="

	logger.Warn(sep)
	logger.Warn("FIRST RUN: Default admin API key created")
	logger.Warn("Key: " + key.KeyValue)
	logger.Warn("Save this key - it will not be shown again")
	logger.Warn(sep)

	fmt.Fprintln(os.Stderr, "")
	fmt.Fprintln(os.Stderr, sep)
	fmt.Fprintln(os.Stderr, "  FIRST RUN: Default admin API key created:")
	fmt.Fprintln(os.Stderr, "")
	fmt.Fprintf(os.Stderr, "  %s\n", key.KeyValue)
	fmt.Fprintln(os.Stderr, "")
	fmt.Fprintln(os.Stderr, "  Save this key - it will not be shown again")
	fmt.Fprintln(os.Stderr, sep)
	fmt.Fprintln(os.Stderr, "")

	os.WriteFile("data/admin_key.txt", []byte(key.KeyValue), 0600)
}

func findAvailablePort() int {
	for port := 5100; port < 5200; port++ {
		addr := fmt.Sprintf("127.0.0.1:%d", port)
		ln, err := net.Listen("tcp", addr)
		if err == nil {
			ln.Close()
			return port
		}
	}
	return 5100
}

func printStartupBanner(addr string, webUIPort int, tlsEnabled bool) {
	displayAddr := strings.Replace(addr, "0.0.0.0", "localhost", 1)
	scheme := "http"
	if tlsEnabled {
		scheme = "https"
	}
	const sep = "═══════════════════════════════════════════════"
	const reset = "\033[0m"
	const bold = "\033[1m"
	const cyan = "\033[36m"
	const green = "\033[32m"
	const yellow = "\033[33m"

	b := func(s string) string { return bold + s + reset }
	c := func(s string) string { return cyan + s + reset }
	g := func(s string) string { return green + s + reset }
	y := func(s string) string { return yellow + s + reset }

	fmt.Fprintln(os.Stderr, "")
	fmt.Fprintln(os.Stderr, c(sep))
	fmt.Fprintln(os.Stderr, c("  "+b("QwenPortal")+"  --  LLM API Gateway"))
	fmt.Fprintln(os.Stderr, c(sep))
	fmt.Fprintln(os.Stderr, "")
	fmt.Fprintln(os.Stderr, "  "+b("All services on one port:")+"  "+g(scheme+"://"+displayAddr))
	fmt.Fprintln(os.Stderr, "")
	fmt.Fprintln(os.Stderr, "  "+b("Web UI:"))
	fmt.Fprintf(os.Stderr, "    %-36s %s\n", g(scheme+"://"+displayAddr+"/"), "Dashboard, providers, API keys, stats")
	fmt.Fprintln(os.Stderr, "")
	fmt.Fprintln(os.Stderr, "  "+b("API:"))
	fmt.Fprintf(os.Stderr, "    %-36s %s\n", c("/v1/chat/completions"), "OpenAI-compatible chat completion")
	fmt.Fprintf(os.Stderr, "    %-36s %s\n", c("/v1/responses"), "OpenAI Responses API")
	fmt.Fprintf(os.Stderr, "    %-36s %s\n", c("/v1/embeddings"), "OpenAI-compatible embeddings")
	fmt.Fprintf(os.Stderr, "    %-36s %s\n", c("/v1/models"), "List available models")
	fmt.Fprintf(os.Stderr, "    %-36s %s\n", c("/v1/messages"), "Claude-compatible messages")
	fmt.Fprintf(os.Stderr, "    %-36s %s\n", c("/v1/chat/completions (Gemini)"), "Gemini via OpenAI format")
	fmt.Fprintln(os.Stderr, "")
	fmt.Fprintln(os.Stderr, "  "+b("Usage examples:"))
	fmt.Fprintf(os.Stderr, "    curl -X POST %s://%s/v1/chat/completions \\\n", scheme, displayAddr)
	fmt.Fprintln(os.Stderr, `      -d '{"model":"<model>","messages":[{"role":"user","content":"hello"}]}'`)
	fmt.Fprintln(os.Stderr, "")
	fmt.Fprintf(os.Stderr, "    from openai import OpenAI\n")
	fmt.Fprintf(os.Stderr, "    client = OpenAI(base_url=%q)\n", scheme+"://"+displayAddr+"/v1")
	fmt.Fprintln(os.Stderr, "")
	fmt.Fprintln(os.Stderr, "  "+b("Admin key saved to:")+"  "+y("data/admin_key.txt"))
	fmt.Fprintln(os.Stderr, "  "+b("Admin password saved to:")+"  "+y("data/password.txt"))
	fmt.Fprintln(os.Stderr, c(sep))
	fmt.Fprintln(os.Stderr, "")
}

func findWebUIDir() string {
	candidates := []string{"webui", "../webui", "./webui"}
	for _, d := range candidates {
		if _, err := os.Stat(filepath.Join(d, "app.py")); err == nil {
			return d
		}
	}
	return "webui"
}

func proxyToFlask(c *gin.Context, flaskPort int) {
	if flaskPort == 0 {
		c.String(http.StatusNotFound, "web ui not available")
		return
	}

	target := fmt.Sprintf("http://127.0.0.1:%d%s", flaskPort, c.Request.URL.Path)
	if c.Request.URL.RawQuery != "" {
		target += "?" + c.Request.URL.RawQuery
	}

	req, err := http.NewRequestWithContext(c.Request.Context(), c.Request.Method, target, c.Request.Body)
	if err != nil {
		c.Status(http.StatusBadGateway)
		return
	}

	for k, v := range c.Request.Header {
		req.Header[k] = v
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		c.String(http.StatusBadGateway, "web ui error: %v", err)
		return
	}
	defer resp.Body.Close()

	for k, v := range resp.Header {
		for _, vv := range v {
			c.Header(k, vv)
		}
	}
	c.Status(resp.StatusCode)
	io.Copy(c.Writer, resp.Body)
}
