package main

import (
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"sync/atomic"
	"syscall"
	"time"
)

var Version = "1.1.2"
const RepoName = "TarsOpenWebUI"

var startTime = time.Now()
var requestCount int64
var errorCount int64

type Client struct {
	Endpoint string
	APIKey   string
	Client   *http.Client
}

func NewClient(endpoint, apiKey string) *Client {
	if endpoint == "" {
		endpoint = "https://api.example.com"
	}
	return &Client{
		Endpoint: strings.TrimRight(endpoint, "/"),
		APIKey:   apiKey,
		Client:   &http.Client{Timeout: 30 * time.Second},
	}
}

func parseDurationEnv(key, defaultVal string) time.Duration {
	s := os.Getenv(key)
	if s == "" { s = defaultVal }
	d, err := time.ParseDuration(s)
	if err != nil {
		log.Printf("[警告] %s 格式不对（%s），使用默认值 %s", key, s, defaultVal)
		return mustParseDuration(defaultVal)
	}
	return d
}

func mustParseDuration(s string) time.Duration {
	d, _ := time.ParseDuration(s)
	return d
}

// reqWithRetry performs HTTP request with exponential backoff (3 attempts).
func (c *Client) reqWithRetry(ctx context.Context, method, path string, body interface{}) (map[string]interface{}, error) {
	var lastErr error
	for attempt := 0; attempt < 3; attempt++ {
		if attempt > 0 {
			backoff := time.Duration(1<<attempt) * time.Second
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(backoff):
			}
		}
		res, err := c.req(ctx, method, path, body)
		if err == nil {
			return res, nil
		}
		lastErr = err
	}
	return nil, fmt.Errorf("请求3次均失败，最后一次错误：%v", lastErr)
}

func (c *Client) req(ctx context.Context, method, path string, body interface{}) (map[string]interface{}, error) {
	var bodyR io.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		bodyR = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.Endpoint+path, bodyR)
	if err != nil { return nil, err }
	if c.APIKey != "" {
		req.Header.Set("Authorization", "Bearer "+c.APIKey)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := c.Client.Do(req)
	if err != nil { return nil, err }
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		b, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("HTTP %d: %s", resp.StatusCode, string(b))
	}
	var result map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return map[string]interface{}{"raw": "non-JSON response"}, nil
	}
	return result, nil
}

func loadConfig() map[string]string {
	return map[string]string{
		"endpoint": os.Getenv("TARS_MCP_ENDPOINT"),
		"api_key":  os.Getenv("TARS_MCP_API_KEY"),
	}
}

func validateConfig(cfg map[string]string) error {
	if cfg["endpoint"] == "" {
		return fmt.Errorf("环境变量 TARS_MCP_ENDPOINT 未设置，请配置上游 API 地址")
	}
	return nil
}

func main() {
	version := flag.Bool("version", false, "print version and exit")
	flag.Parse()
	if *version {
		fmt.Printf("%s v%s\n", RepoName, Version)
		return
	}
	cfg := loadConfig()
	if err := validateConfig(cfg); err != nil {
		log.Fatalf("[配置错误] %v", err)
	}
	client := NewClient(cfg["endpoint"], cfg["api_key"])
	readTimeout := parseDurationEnv("TARS_READ_TIMEOUT", "30s")
	writeTimeout := parseDurationEnv("TARS_WRITE_TIMEOUT", "30s")
	log.Printf("[%s] 启动中 版本=%s 上游=%s read_timeout=%s write_timeout=%s",
		RepoName, Version, cfg["endpoint"], readTimeout, writeTimeout)
	mode := os.Getenv("TARS_MCP_MODE")
	if mode == "" { mode = "stdio" }
	srv := &http.Server{
		Addr:         ":8080",
		ReadTimeout:  readTimeout,
		WriteTimeout: writeTimeout,
	}
	http.HandleFunc("/health", loggingMiddleware(func(w http.ResponseWriter, r *http.Request) { withClient(client, healthHandler)(w, r) }))
	http.HandleFunc("/tools", loggingMiddleware(func(w http.ResponseWriter, r *http.Request) { withClient(client, toolsHandler)(w, r) }))
	http.HandleFunc("/invoke", loggingMiddleware(func(w http.ResponseWriter, r *http.Request) { withClient(client, invokeHandler)(w, r) }))
	http.HandleFunc("/metrics", loggingMiddleware(metricsHandler))
	http.HandleFunc("/version", loggingMiddleware(versionHandler))
	go func() {
		sigCh := make(chan os.Signal, 1)
		signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
		<-sigCh
		log.Printf("[%s] 正在关闭...", RepoName)
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := srv.Shutdown(ctx); err != nil {
			log.Printf("[%s] 关闭超时: %v", RepoName, err)
			_ = srv.Close()
		}
	}()
	if mode == "stdio" {
		runStdioServer(client)
		return
	}
	log.Printf("[%s] HTTP 服务已启动 %s", RepoName, srv.Addr)
	if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		log.Fatalf("服务异常：%v", err)
	}
}

func loggingMiddleware(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt64(&requestCount, 1)
		start := time.Now()
		rw := &responseRecorder{ResponseWriter: w, statusCode: http.StatusOK}
		next(rw, r)
		if rw.statusCode >= 400 {
			atomic.AddInt64(&errorCount, 1)
		}
		log.Printf("[%s] %s %s -> %d (%s)", RepoName, r.Method, r.URL.Path, rw.statusCode, time.Since(start))
	}
}

type responseRecorder struct {
	http.ResponseWriter
	statusCode int
}

func (rr *responseRecorder) WriteHeader(code int) {
	rr.statusCode = code
	rr.ResponseWriter.WriteHeader(code)
}

func withClient(client *Client, fn func(http.ResponseWriter, *http.Request, *Client)) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")
		if r.Method == http.MethodOptions { w.WriteHeader(http.StatusNoContent); return }
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("X-Frame-Options", "DENY")
		fn(w, r, client)
	}
}

func healthHandler(w http.ResponseWriter, r *http.Request, client *Client) {
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()
	up := map[string]interface{}{"healthy": false}
	if _, err := client.reqWithRetry(ctx, "GET", "/health", nil); err == nil { up["healthy"] = true }
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"status":   "ok",
		"version":  Version,
		"name":     RepoName,
		"upstream": up,
		"uptime":   time.Since(startTime).Round(time.Second).String(),
		"time":     time.Now().UTC().Format(time.RFC3339),
	})
}

func metricsHandler(w http.ResponseWriter, r *http.Request) {
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"requests_total": atomic.LoadInt64(&requestCount),
		"errors_total":   atomic.LoadInt64(&errorCount),
		"uptime":         time.Since(startTime).Round(time.Second).String(),
		"version":        Version,
	})
}

func versionHandler(w http.ResponseWriter, r *http.Request) {
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"name":    RepoName,
		"version": Version,
		"go":      "1.22",
	})
}

func toolsHandler(w http.ResponseWriter, r *http.Request, client *Client) {
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"tools": []map[string]interface{}{
			{
				"name":        "openwebui_list_models",
				"description": "列出 OpenWebUI 中可用的本地模型",
				"inputSchema": map[string]interface{}{
					"type":       "object",
					"properties": map[string]interface{}{},
					"required":   []interface{}{},
				},
			},
			{
				"name":        "openwebui_chat",
				"description": "向指定模型发送对话请求并获取回复",
				"inputSchema": map[string]interface{}{
					"type": "object",
					"properties": map[string]interface{}{
						"messages": map[string]interface{}{"type": "array", "description": "对话消息列表，每条包含 role 和 content"},
						"model":    map[string]interface{}{"type": "string", "description": "要使用的模型名称"},
					},
					"required": []interface{}{"model", "messages"},
				},
			},
		},
	})
}

func invokeHandler(w http.ResponseWriter, r *http.Request, client *Client) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		_ = json.NewEncoder(w).Encode(map[string]interface{}{"error": "只接受 POST 请求，请使用 POST 方法调用工具"})
		return
	}
	var req struct {
		Tool   string                 `json:"tool"`
		Params map[string]interface{} `json:"params"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]interface{}{"error": fmt.Sprintf("请求格式不对，请检查 JSON：%v", err)})
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 60*time.Second)
	defer cancel()
	switch req.Tool {
	case "openwebui_list_models":
		result, err := client.reqWithRetry(ctx, "GET", "/api/models", nil)
		if err != nil {
			w.WriteHeader(http.StatusBadGateway)
			_ = json.NewEncoder(w).Encode(map[string]interface{}{"error": fmt.Sprintf("连接上游失败：%v", err)})
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]interface{}{"result": result})
	case "openwebui_chat":
		if req.Params["model"] == nil {
			w.WriteHeader(http.StatusBadRequest)
			_ = json.NewEncoder(w).Encode(map[string]interface{}{"error": "缺少参数：model（请指定要使用的模型名称）"})
			return
		}
		if req.Params["messages"] == nil {
			w.WriteHeader(http.StatusBadRequest)
			_ = json.NewEncoder(w).Encode(map[string]interface{}{"error": "缺少参数：messages（请提供对话消息列表）"})
			return
		}
		body := map[string]interface{}{"messages": req.Params["messages"], "model": req.Params["model"]}
		result, err := client.reqWithRetry(ctx, "POST", "/api/chat/completions", body)
		if err != nil {
			w.WriteHeader(http.StatusBadGateway)
			_ = json.NewEncoder(w).Encode(map[string]interface{}{"error": fmt.Sprintf("对话请求失败：%v", err)})
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]interface{}{"result": result})
	default:
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]interface{}{"error": fmt.Sprintf("暂不支持该工具：%s（可用工具：openwebui_list_models、openwebui_chat）", req.Tool)})
	}
}

func runStdioServer(client *Client) {
	log.Printf("[%s] stdio 模式已启动", RepoName)
	dec := json.NewDecoder(os.Stdin)
	enc := json.NewEncoder(os.Stdout)
	for {
		var msg map[string]interface{}
		if err := dec.Decode(&msg); err != nil { return }
		id := msg["id"]
		method, _ := msg["method"].(string)
		var result map[string]interface{}
		switch method {
		case "tools/list":
			result = map[string]interface{}{"tools": []map[string]interface{}{
				{"name": "openwebui_list_models", "description": "列出 OpenWebUI 中可用的本地模型"},
				{"name": "openwebui_chat", "description": "向指定模型发送对话请求并获取回复"},
			}}
		case "tools/call":
			params, _ := msg["params"].(map[string]interface{})
			toolName, _ := params["name"].(string)
			args, _ := params["arguments"].(map[string]interface{})
			ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
			result = handleStdioTool(ctx, client, toolName, args)
			cancel()
		default:
			result = map[string]interface{}{"message": fmt.Sprintf("%s v%s 就绪", RepoName, Version)}
		}
		_ = enc.Encode(map[string]interface{}{"jsonrpc": "2.0", "id": id, "result": result})
	}
}

func handleStdioTool(ctx context.Context, client *Client, toolName string, args map[string]interface{}) map[string]interface{} {
	switch toolName {
	case "openwebui_list_models":
		result, err := client.reqWithRetry(ctx, "GET", "/api/models", nil)
		if err != nil {
			return map[string]interface{}{"content": []map[string]string{{"type": "text", "text": fmt.Sprintf("获取模型列表失败：%v", err)}}}
		}
		data, _ := json.Marshal(result)
		return map[string]interface{}{"content": []map[string]string{{"type": "text", "text": string(data)}}}
	case "openwebui_chat":
		if args["model"] == nil {
			return map[string]interface{}{"content": []map[string]string{{"type": "text", "text": "缺少参数：model（请指定要使用的模型名称）"}}}
		}
		if args["messages"] == nil {
			return map[string]interface{}{"content": []map[string]string{{"type": "text", "text": "缺少参数：messages（请提供对话消息列表）"}}}
		}
		result, err := client.reqWithRetry(ctx, "POST", "/api/chat/completions", map[string]interface{}{"messages": args["messages"], "model": args["model"]})
		if err != nil {
			return map[string]interface{}{"content": []map[string]string{{"type": "text", "text": fmt.Sprintf("对话请求失败：%v", err)}}}
		}
		data, _ := json.Marshal(result)
		return map[string]interface{}{"content": []map[string]string{{"type": "text", "text": string(data)}}}
	default:
		return map[string]interface{}{"content": []map[string]string{{"type": "text", "text": fmt.Sprintf("暂不支持该工具：%s（可用工具：openwebui_list_models、openwebui_chat）", toolName)}}}
	}
}
