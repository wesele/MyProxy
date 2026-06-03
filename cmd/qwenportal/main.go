package main

import (
	"context"
	"fmt"
	"io"
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

	if err := db.Init(cfg.Database.Path); err != nil {
		logger.Fatal("failed to init database", zap.Error(err))
	}
	defer db.Close()

	bootstrapAdminKey(logger)

	router_ := proxy.NewRouter()
	if err := router_.Refresh(); err != nil {
		logger.Warn("failed to refresh router", zap.Error(err))
	}

	forwarder := proxy.NewForwarder(logger)

	openAIHandler := api.NewOpenAIHandler(forwarder, router_, logger)
	geminiHandler := api.NewGeminiHandler(forwarder, router_, logger)
	openAIHandler.SetGeminiHandler(geminiHandler)
	claudeHandler := api.NewClaudeHandler(forwarder, router_, logger)
	adminHandler := api.NewAdminHandler(logger, router_)

	gin.SetMode(gin.ReleaseMode)
	r := gin.New()
	r.Use(gin.Recovery())
	r.Use(middleware.CORS())
	r.Use(middleware.RequestLogger(logger))

	r.GET("/", func(c *gin.Context) {
		c.Redirect(http.StatusFound, "/admin/dashboard")
	})
	r.GET("/v1/models", openAIHandler.ListModels)
	r.POST("/v1/chat/completions", openAIHandler.ChatCompletions)
	r.POST("/v1/embeddings", openAIHandler.Embeddings)
	r.POST("/v1/messages", claudeHandler.Messages)

	admin := r.Group("/admin/api", middleware.AdminAuth())
	{
		admin.GET("/providers", adminHandler.ListProviders)
		admin.POST("/providers", adminHandler.CreateProvider)
		admin.GET("/providers/:id", adminHandler.GetProvider)
		admin.PUT("/providers/:id", adminHandler.UpdateProvider)
		admin.DELETE("/providers/:id", adminHandler.DeleteProvider)
		admin.GET("/providers/export", adminHandler.ExportProviders)
		admin.POST("/providers/import", adminHandler.ImportProviders)

		admin.GET("/keys", adminHandler.ListApiKeys)
		admin.POST("/keys", adminHandler.CreateApiKey)
		admin.PUT("/keys/:id", adminHandler.UpdateApiKey)
		admin.DELETE("/keys/:id", adminHandler.DeleteApiKey)

		admin.GET("/stats", adminHandler.GetStats)
		admin.GET("/logs", adminHandler.GetModelLogs)
		admin.POST("/providers/fetch-models", adminHandler.FetchProviderModels)
		admin.POST("/providers/test", adminHandler.TestProvider)
		admin.POST("/models/test", adminHandler.TestModels)
		admin.POST("/training/start", adminHandler.TrainingStart)
		admin.POST("/training/stop", adminHandler.TrainingStop)
		admin.GET("/training/stats", adminHandler.TrainingStats)
		admin.GET("/training/active", adminHandler.TrainingActive)
	}

	var flaskCmd *exec.Cmd
	if cfg.WebUI.Enabled {
		flaskCmd = startFlask(cfg, logger)
	}

	r.NoRoute(func(c *gin.Context) {
		if strings.HasPrefix(c.Request.URL.Path, "/admin") {
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

	printStartupBanner(addr, cfg.WebUI.Port)

	go func() {
		logger.Info("server starting", zap.String("addr", addr))
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			logger.Fatal("server error", zap.Error(err))
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

func startFlask(cfg *config.Config, logger *zap.Logger) *exec.Cmd {
	webUIPort := cfg.WebUI.Port
	if webUIPort == 0 {
		webUIPort = findAvailablePort()
	}

	webUIDir := findWebUIDir()

	cmd := exec.Command(cfg.WebUI.Python, "app.py")
	cmd.Dir = webUIDir
	cmd.Env = append(os.Environ(),
		"FLASK_PORT="+strconv.Itoa(webUIPort),
		"FLASK_DEBUG=0",
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

func bootstrapAdminKey(logger *zap.Logger) {
	keys, err := db.ListApiKeys()
	if err != nil || len(keys) > 0 {
		return
	}

	key, err := db.CreateApiKey("admin", 0)
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

func printStartupBanner(addr string, webUIPort int) {
	displayAddr := strings.Replace(addr, "0.0.0.0", "localhost", 1)
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
	fmt.Fprintln(os.Stderr, "  "+b("All services on one port:")+"  "+g("http://"+displayAddr))
	fmt.Fprintln(os.Stderr, "")
	fmt.Fprintln(os.Stderr, "  "+b("Web UI:"))
	fmt.Fprintf(os.Stderr, "    %-36s %s\n", g("http://"+displayAddr+"/"), "Dashboard, providers, API keys, stats")
	fmt.Fprintln(os.Stderr, "")
	fmt.Fprintln(os.Stderr, "  "+b("API:"))
	fmt.Fprintf(os.Stderr, "    %-36s %s\n", c("/v1/chat/completions"), "OpenAI-compatible chat completion")
	fmt.Fprintf(os.Stderr, "    %-36s %s\n", c("/v1/embeddings"), "OpenAI-compatible embeddings")
	fmt.Fprintf(os.Stderr, "    %-36s %s\n", c("/v1/models"), "List available models")
	fmt.Fprintf(os.Stderr, "    %-36s %s\n", c("/v1/messages"), "Claude-compatible messages")
	fmt.Fprintf(os.Stderr, "    %-36s %s\n", c("/v1/chat/completions (Gemini)"), "Gemini via OpenAI format")
	fmt.Fprintln(os.Stderr, "")
	fmt.Fprintln(os.Stderr, "  "+b("Usage examples:"))
	fmt.Fprintf(os.Stderr, "    curl -X POST http://%s/v1/chat/completions \\\n", displayAddr)
	fmt.Fprintln(os.Stderr, `      -d '{"model":"<model>","messages":[{"role":"user","content":"hello"}]}'`)
	fmt.Fprintln(os.Stderr, "")
	fmt.Fprintf(os.Stderr, "    from openai import OpenAI\n")
	fmt.Fprintf(os.Stderr, "    client = OpenAI(base_url=%q)\n", "http://"+displayAddr+"/v1")
	fmt.Fprintln(os.Stderr, "")
	fmt.Fprintln(os.Stderr, "  "+b("Admin key saved to:")+"  "+y("data/admin_key.txt"))
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
