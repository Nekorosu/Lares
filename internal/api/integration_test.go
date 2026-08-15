package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"lares/internal/auth"
	"lares/internal/config"
	"lares/internal/db"
)

func setupTestServer(t *testing.T) (*Server, string) {
	tempDir, err := os.MkdirTemp("", "lares_test_")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}

	cfg := &config.Config{
		Paths: config.Paths{
			DataDir:     filepath.Join(tempDir, "data"),
			TmpDir:      filepath.Join(tempDir, "tmp"),
			DBPath:      filepath.Join(tempDir, "lares.db"),
			SecurityLog: filepath.Join(tempDir, "security.log"),
		},
		Limits: config.Limits{
			DefaultStorageQuotaGB:         1,
			DefaultMonthlyUploadLimitGB:   1,
			DefaultMonthlyDownloadLimitGB: 1,
			DefaultMaxFileSizeGB:          1,
		},
		Secrets: config.Secrets{
			SessionSecret: "test-secret",
			IPSalt:        "test-salt",
		},
		Network: config.Network{LocalCIDRs: []string{"127.0.0.1/32"}},
	}

	os.MkdirAll(cfg.Paths.DataDir, 0755)
	os.MkdirAll(cfg.Paths.TmpDir, 0755)

	database, err := db.InitDB(cfg.Paths.DBPath)
	if err != nil {
		t.Fatalf("Failed to init DB: %v", err)
	}

	srv, err := NewServer(cfg, database)
	if err != nil {
		t.Fatalf("Failed to create server: %v", err)
	}

	return srv, tempDir
}

func insertTestUserAndSession(t *testing.T, srv *Server, token string) int64 {
	personID := int64(1)
	_, err := srv.db.Exec(`INSERT INTO people (id, label, enabled, storage_quota_bytes, monthly_upload_limit_bytes, monthly_download_limit_bytes, max_file_size_bytes, max_concurrent_uploads, created_at) VALUES (?, 'TestUser', 1, 1000000, 1000000, 1000000, 1000000, 10, ?)`, personID, time.Now().UTC())
	if err != nil {
		t.Fatalf("Failed to insert person: %v", err)
	}

	tokenHash := auth.HashWithSalt(token, srv.cfg.Secrets.SessionSecret)
	now := time.Now().UTC()
	_, err = srv.db.Exec(`INSERT INTO device_sessions (person_id, name, session_token_hash, created_at, last_used_at, last_ip_hash, last_user_agent_hash, idle_expires_at, absolute_expires_at, revoked) VALUES (?, 'Test', ?, ?, ?, '', '', ?, ?, 0)`, personID, tokenHash, now, now, now.Add(1*time.Hour), now.Add(1*time.Hour))
	if err != nil {
		t.Fatalf("Failed to insert session: %v", err)
	}
	return personID
}

func TestUnauthenticatedDirectUpload(t *testing.T) {
	srv, tempDir := setupTestServer(t)
	defer os.RemoveAll(tempDir)
	defer srv.db.Close()

	router := srv.Routes()

	body := bytes.NewReader([]byte("malicious payload"))
	req := httptest.NewRequest("POST", "/api/files/upload/direct?filename=test.txt", body)
	rr := httptest.NewRecorder()

	router.ServeHTTP(rr, req)

	if rr.Code != http.StatusUnauthorized {
		t.Errorf("Expected 401 Unauthorized for direct upload, got %d", rr.Code)
	}

	files, _ := os.ReadDir(filepath.Join(tempDir, "data"))
	if len(files) > 0 {
		t.Errorf("Expected no files created, but found %d files in data dir", len(files))
	}
}

func TestUploadReservation(t *testing.T) {
	srv, tempDir := setupTestServer(t)
	defer os.RemoveAll(tempDir)
	defer srv.db.Close()

	token := "valid_test_token"
	insertTestUserAndSession(t, srv, token)

	router := srv.Routes()

	// 1. Anonymous -> Rejected
	reqAnon := httptest.NewRequest("POST", "/api/files/upload/reserve", bytes.NewReader([]byte(`{"filename":"test.txt","size":100}`)))
	reqAnon.Header.Set("Content-Type", "application/json")
	rrAnon := httptest.NewRecorder()
	router.ServeHTTP(rrAnon, reqAnon)
	if rrAnon.Code != http.StatusUnauthorized {
		t.Errorf("Anonymous reservation should be rejected, got %d", rrAnon.Code)
	}

	// 2. Normal user -> Allowed
	reqUser := httptest.NewRequest("POST", "/api/files/upload/reserve", bytes.NewReader([]byte(`{"filename":"test2.txt","size":100}`)))
	reqUser.Header.Set("Content-Type", "application/json")
	reqUser.AddCookie(&http.Cookie{Name: "homeshare_session", Value: token})

	csrfToken := "test-csrf-token"
	reqUser.AddCookie(&http.Cookie{Name: "homeshare_csrf", Value: csrfToken})
	reqUser.Header.Set("X-CSRF-Token", csrfToken)

	rrUser := httptest.NewRecorder()
	router.ServeHTTP(rrUser, reqUser)
	if rrUser.Code != http.StatusOK {
		t.Errorf("User reservation should be allowed, got %d", rrUser.Code)
	}
}

func TestQueryStringTokenAuthentication(t *testing.T) {
	srv, tempDir := setupTestServer(t)
	defer os.RemoveAll(tempDir)
	defer srv.db.Close()

	token := "query_test_token"
	insertTestUserAndSession(t, srv, token)

	router := srv.Routes()

	// 1. Query string token -> NOT authenticated
	reqQuery := httptest.NewRequest("GET", "/api/auth/me?token="+token, nil)
	rrQuery := httptest.NewRecorder()
	router.ServeHTTP(rrQuery, reqQuery)

	if rrQuery.Code == http.StatusOK {
		var res map[string]interface{}
		json.Unmarshal(rrQuery.Body.Bytes(), &res)
		if auth, ok := res["authenticated"].(bool); ok && auth {
			t.Errorf("Query string token should NOT authenticate, but it did")
		}
	} else if rrQuery.Code != http.StatusUnauthorized {
		t.Errorf("Unexpected status code %d", rrQuery.Code)
	}

	// 2. Cookie token -> Authenticated
	reqCookie := httptest.NewRequest("GET", "/api/auth/me", nil)
	reqCookie.AddCookie(&http.Cookie{Name: "homeshare_session", Value: token})
	rrCookie := httptest.NewRecorder()
	router.ServeHTTP(rrCookie, reqCookie)

	if rrCookie.Code != http.StatusOK {
		t.Errorf("Cookie token should authenticate, got %d", rrCookie.Code)
	} else {
		var res map[string]interface{}
		json.Unmarshal(rrCookie.Body.Bytes(), &res)
		if auth, ok := res["authenticated"].(bool); !ok || !auth {
			t.Errorf("Cookie token should authenticate, but it didn't")
		}
	}
}
