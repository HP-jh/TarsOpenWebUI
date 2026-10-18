package main

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestVersion(t *testing.T) {
	if Version != "1.1.0" {
		t.Fatalf("expected version 1.1.0, got %s", Version)
	}
}

func TestNewClient_DefaultEndpoint(t *testing.T) {
	c := NewClient("", "")
	if c.Endpoint != "https://api.example.com" {
		t.Fatalf("expected default endpoint, got %s", c.Endpoint)
	}
}

func TestValidateConfig_MissingEndpoint(t *testing.T) {
	cfg := map[string]string{"endpoint": ""}
	if err := validateConfig(cfg); err == nil {
		t.Fatal("expected error for missing endpoint")
	}
}

func TestValidateConfig_OK(t *testing.T) {
	cfg := map[string]string{"endpoint": "http://localhost:3000"}
	if err := validateConfig(cfg); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestHealthHandler(t *testing.T) {
	client := NewClient("", "")
	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	w := httptest.NewRecorder()
	loggingMiddleware(withClient(client, healthHandler))(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	var resp map[string]interface{}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if resp["status"] != "ok" {
		t.Fatalf("expected ok, got %v", resp["status"])
	}
	if _, ok := resp["uptime"]; !ok {
		t.Fatal("expected uptime field")
	}
}

func TestToolsHandler(t *testing.T) {
	client := NewClient("", "")
	req := httptest.NewRequest(http.MethodGet, "/tools", nil)
	w := httptest.NewRecorder()
	loggingMiddleware(withClient(client, toolsHandler))(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	var resp map[string]interface{}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	tools, ok := resp["tools"].([]interface{})
	if !ok || len(tools) == 0 {
		t.Fatal("expected tools")
	}
}

func TestInvokeHandler_MethodNotAllowed(t *testing.T) {
	client := NewClient("", "")
	req := httptest.NewRequest(http.MethodGet, "/invoke", nil)
	w := httptest.NewRecorder()
	loggingMiddleware(withClient(client, invokeHandler))(w, req)
	if w.Code != http.StatusMethodNotAllowed {
		t.Fatalf("expected 405, got %d", w.Code)
	}
	var resp map[string]interface{}
	json.Unmarshal(w.Body.Bytes(), &resp)
	msg, _ := resp["error"].(string)
	if !strings.Contains(msg, "POST") {
		t.Fatalf("expected friendly POST message, got %v", msg)
	}
}

func TestInvokeHandler_UnknownTool(t *testing.T) {
	client := NewClient("", "")
	body, _ := json.Marshal(map[string]interface{}{"tool": "xxx_unknown", "params": map[string]interface{}{}})
	req := httptest.NewRequest(http.MethodPost, "/invoke", bytes.NewReader(body))
	w := httptest.NewRecorder()
	loggingMiddleware(withClient(client, invokeHandler))(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
}

func TestInvokeHandler_MissingModel(t *testing.T) {
	client := NewClient("", "")
	body, _ := json.Marshal(map[string]interface{}{"tool": "openwebui_chat", "params": map[string]interface{}{"messages": []interface{}{}}})
	req := httptest.NewRequest(http.MethodPost, "/invoke", bytes.NewReader(body))
	w := httptest.NewRecorder()
	loggingMiddleware(withClient(client, invokeHandler))(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
	var resp map[string]interface{}
	json.Unmarshal(w.Body.Bytes(), &resp)
	msg, _ := resp["error"].(string)
	if !strings.Contains(msg, "model") {
		t.Fatalf("expected model missing message, got %v", msg)
	}
}

func TestClientReq_MockServer(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]interface{}{"status": "ok"})
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()
	client := NewClient(srv.URL, "test-key")
	res, err := client.req(context.Background(), "GET", "/health", nil)
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if res["status"] != "ok" {
		t.Fatalf("expected ok, got %v", res["status"])
	}
}

func TestReqWithRetry_SuccessOnSecond(t *testing.T) {
	attempts := 0
	mux := http.NewServeMux()
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		attempts++
		if attempts == 1 {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		json.NewEncoder(w).Encode(map[string]interface{}{"status": "ok"})
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()
	client := NewClient(srv.URL, "")
	res, err := client.reqWithRetry(context.Background(), "GET", "/health", nil)
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if res["status"] != "ok" {
		t.Fatalf("expected ok, got %v", res["status"])
	}
	if attempts != 2 {
		t.Fatalf("expected 2 attempts, got %d", attempts)
	}
}

func TestResponseRecorder(t *testing.T) {
	w := httptest.NewRecorder()
	rr := &responseRecorder{ResponseWriter: w, statusCode: http.StatusOK}
	rr.WriteHeader(http.StatusCreated)
	if rr.statusCode != http.StatusCreated {
		t.Fatalf("expected 201, got %d", rr.statusCode)
	}
}

func TestLoadConfig(t *testing.T) {
	t.Setenv("TARS_MCP_ENDPOINT", "http://test:3000")
	t.Setenv("TARS_MCP_API_KEY", "secret")
	cfg := loadConfig()
	if cfg["endpoint"] != "http://test:3000" {
		t.Fatalf("unexpected endpoint: %s", cfg["endpoint"])
	}
	if cfg["api_key"] != "secret" {
		t.Fatalf("unexpected api_key: %s", cfg["api_key"])
	}
}

func TestHandleStdioTool_Unknown(t *testing.T) {
	client := NewClient("", "")
	res := handleStdioTool(context.Background(), client, "unknown_tool", nil)
	content, _ := res["content"].([]map[string]string)
	if len(content) == 0 || !strings.Contains(content[0]["text"], "暂不支持") {
		t.Fatalf("expected friendly unknown tool message, got %v", res)
	}
}

func TestHealthHandler_UptimeFormat(t *testing.T) {
	startTime = time.Now().Add(-2 * time.Minute)
	client := NewClient("", "")
	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	w := httptest.NewRecorder()
	withClient(client, healthHandler)(w, req)
	var resp map[string]interface{}
	json.Unmarshal(w.Body.Bytes(), &resp)
	uptime, ok := resp["uptime"].(string)
	if !ok || uptime == "" {
		t.Fatalf("expected uptime string, got %v", resp["uptime"])
	}
}
