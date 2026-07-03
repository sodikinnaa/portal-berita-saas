package httpserver

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	core "porta-berita/internal/cms"
)

func TestInboundMailWebhook_ValidToken(t *testing.T) {
	store := &mockStore{
		settings: map[string]string{
			"mail_webhook_token": "valid_token_abc123",
		},
		users: map[string]*core.User{
			"admin@portal.test": {
				ID:    "user-admin",
				Name:  "Admin Portal",
				Email: "admin@portal.test",
				Role:  "admin",
			},
		},
	}
	server := newTestServer(t, store)

	payload := `{
		"type": "email.received",
		"data": {
			"from": "Sender Name <sender@example.com>",
			"to": ["admin@portal.test"],
			"subject": "Hello Webhook Test",
			"html": "<p>Body HTML</p>",
			"text": "Body Text"
		}
	}`

	req := httptest.NewRequest("POST", "/api/v1/mail/inbound?token=valid_token_abc123", strings.NewReader(payload))
	req.Header.Set("Content-Type", "application/json")

	w := httptest.NewRecorder()
	server.apiInboundMailWebhook(w, req)

	resp := w.Result()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected status OK, got %d", resp.StatusCode)
	}

	// Verify that the email was successfully inserted into mockStore
	if len(store.emails) != 1 {
		t.Errorf("expected 1 email in store, got %d", len(store.emails))
	} else {
		inserted := store.emails[0]
		if inserted.Subject != "Hello Webhook Test" {
			t.Errorf("expected subject 'Hello Webhook Test', got %s", inserted.Subject)
		}
		if inserted.Sender != "sender@example.com" {
			t.Errorf("expected sender 'sender@example.com', got %s", inserted.Sender)
		}
		if inserted.SenderName != "Sender Name" {
			t.Errorf("expected sender name 'Sender Name', got %s", inserted.SenderName)
		}
		if inserted.UserID == nil || *inserted.UserID != "user-admin" {
			t.Errorf("expected mapped user ID 'user-admin', got %v", inserted.UserID)
		}
	}
}

func TestInboundMailWebhook_InvalidToken(t *testing.T) {
	store := &mockStore{
		settings: map[string]string{
			"mail_webhook_token": "valid_token_abc123",
		},
	}
	server := newTestServer(t, store)

	payload := `{}`
	req := httptest.NewRequest("POST", "/api/v1/mail/inbound?token=wrong_token", strings.NewReader(payload))
	req.Header.Set("Content-Type", "application/json")

	w := httptest.NewRecorder()
	server.apiInboundMailWebhook(w, req)

	resp := w.Result()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("expected status Unauthorized (401), got %d", resp.StatusCode)
	}
}

func TestInboundMailWebhook_EmptyTokenInSettings(t *testing.T) {
	store := &mockStore{
		settings: map[string]string{
			"mail_webhook_token": "", // unconfigured
		},
		users: map[string]*core.User{},
	}
	server := newTestServer(t, store)

	payload := `{
		"type": "email.received",
		"data": {
			"from": "sender@example.com",
			"to": ["fallback@portal.test"],
			"subject": "Hello Webhook Test",
			"html": "",
			"text": "Body Text"
		}
	}`

	req := httptest.NewRequest("POST", "/api/v1/mail/inbound", strings.NewReader(payload))
	req.Header.Set("Content-Type", "application/json")

	w := httptest.NewRecorder()
	server.apiInboundMailWebhook(w, req)

	resp := w.Result()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected status OK, got %d", resp.StatusCode)
	}

	// Verify that email is inserted with no user mapping (fallback / user_id is nil)
	if len(store.emails) != 1 {
		t.Errorf("expected 1 email in store, got %d", len(store.emails))
	} else {
		inserted := store.emails[0]
		if inserted.UserID != nil {
			t.Errorf("expected UserID to be nil (fallback), got %s", *inserted.UserID)
		}
	}
}

func TestDeployCloudflareWorker_Success(t *testing.T) {
	// Start local mock Cloudflare API server
	mockCfServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "PUT" {
			t.Errorf("expected PUT request, got %s", r.Method)
		}
		if !strings.HasSuffix(r.URL.Path, "/workers/scripts/email-incoming-webhook") {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		authHeader := r.Header.Get("Authorization")
		if authHeader != "Bearer test_token" {
			t.Errorf("expected Authorization header Bearer test_token, got %s", authHeader)
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"success": true, "errors": []}`))
	}))
	defer mockCfServer.Close()

	// Override CLOUDFLARE_API_BASE environment variable
	t.Setenv("CLOUDFLARE_API_BASE", mockCfServer.URL)

	store := &mockStore{
		settings: map[string]string{
			"mail_webhook_token": "valid_token_123",
		},
	}
	server := newTestServer(t, store)

	// Build POST request with Form values
	form := url.Values{}
	form.Set("cf_account_id", "test_account")
	form.Set("cf_api_token", "test_token")

	req := httptest.NewRequest("POST", "/dashboard/mail/cloudflare/deploy-worker", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	// Inject admin user into request context
	ctx := withUser(req.Context(), &core.User{ID: "admin-id", Role: core.RoleAdmin})
	req = req.WithContext(ctx)

	w := httptest.NewRecorder()
	server.deployCloudflareWorkerHandler(w, req)

	resp := w.Result()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected status OK, got %d", resp.StatusCode)
	}

	// Verify settings updated
	settings := store.GetSettings()
	if settings["cloudflare_account_id"] != "test_account" {
		t.Errorf("expected cloudflare_account_id to be test_account, got %s", settings["cloudflare_account_id"])
	}
	if settings["cloudflare_api_token"] != "test_token" {
		t.Errorf("expected cloudflare_api_token to be test_token, got %s", settings["cloudflare_api_token"])
	}
}

func TestDeployCloudflareWorker_AuthError(t *testing.T) {
	// Start local mock Cloudflare API server that returns 403
	mockCfServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte(`{"success":false,"errors":[{"code":10000,"message":"Authentication error"}],"messages":[],"result":null}`))
	}))
	defer mockCfServer.Close()

	// Override CLOUDFLARE_API_BASE environment variable
	t.Setenv("CLOUDFLARE_API_BASE", mockCfServer.URL)

	store := &mockStore{
		settings: map[string]string{
			"mail_webhook_token": "valid_token_123",
		},
	}
	server := newTestServer(t, store)

	// Build POST request with Form values
	form := url.Values{}
	form.Set("cf_account_id", "test_account")
	form.Set("cf_api_token", "bad_token")

	req := httptest.NewRequest("POST", "/dashboard/mail/cloudflare/deploy-worker", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	// Inject admin user into request context
	ctx := withUser(req.Context(), &core.User{ID: "admin-id", Role: core.RoleAdmin})
	req = req.WithContext(ctx)

	w := httptest.NewRecorder()
	server.deployCloudflareWorkerHandler(w, req)

	resp := w.Result()
	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("expected status BadRequest (400), got %d", resp.StatusCode)
	}

	// Verify response contains the Cloudflare 403 Authentication error logs
	var res map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&res); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	logs, ok := res["logs"].([]any)
	if !ok {
		t.Fatalf("logs missing in response")
	}

	foundError := false
	for _, log := range logs {
		logStr, _ := log.(string)
		if strings.Contains(logStr, "Cloudflare mengembalikan status 403") {
			foundError = true
			break
		}
	}
	if !foundError {
		t.Errorf("expected error log 'Cloudflare mengembalikan status 403' to be returned in logs, got: %v", logs)
	}
}
