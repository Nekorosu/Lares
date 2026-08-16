package api

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"fmt"
	"mime/multipart"
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
			DefaultExpiryDays:             14,
			MaxConcurrentUploads:          10,
		},
		Secrets: config.Secrets{
			SessionSecret: "test-secret",
			IPSalt:        "test-salt",
		},
		Sessions: config.Sessions{
			UserIdleDays:      1,
			UserAbsoluteDays:  1,
			AdminIdleHours:    1,
			AdminAbsoluteDays: 1,
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

func insertTestAdminSession(t *testing.T, srv *Server, token string) {
	t.Helper()
	now := time.Now().UTC()
	_, err := srv.db.Exec(`INSERT INTO admin_users (id, username, password_hash, totp_secret, totp_enabled, created_at)
		VALUES (1, 'admin', 'hash', 'secret', 0, ?)`, now)
	if err != nil {
		t.Fatalf("Failed to insert admin: %v", err)
	}
	tokenHash := auth.HashWithSalt(token, srv.cfg.Secrets.SessionSecret)
	_, err = srv.db.Exec(`INSERT INTO device_sessions
		(admin_id, is_admin, name, session_token_hash, created_at, last_used_at, last_ip_hash, last_user_agent_hash, idle_expires_at, absolute_expires_at, revoked)
		VALUES (1, 1, 'Admin Test', ?, ?, ?, '', '', ?, ?, 0)`, tokenHash, now, now, now.Add(time.Hour), now.Add(time.Hour))
	if err != nil {
		t.Fatalf("Failed to insert admin session: %v", err)
	}
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

func TestUploadReservationRespectsKeepForeverPermission(t *testing.T) {
	for _, endpoint := range []string{"/api/uploads", "/api/files/upload/reserve"} {
		t.Run(endpoint, func(t *testing.T) {
			srv, tempDir := setupTestServer(t)
			defer os.RemoveAll(tempDir)
			defer srv.db.Close()

			token := "keep_forever_token"
			personID := insertTestUserAndSession(t, srv, token)
			if _, err := srv.db.Exec("UPDATE people SET allow_user_keep_forever = 1 WHERE id = ?", personID); err != nil {
				t.Fatal(err)
			}

			request := func(filename string) int {
				body := bytes.NewBufferString(`{"filename":"` + filename + `","size":100,"keep_forever":true}`)
				req := httptest.NewRequest(http.MethodPost, endpoint, body)
				req.Header.Set("Content-Type", "application/json")
				req.AddCookie(&http.Cookie{Name: "homeshare_session", Value: token})
				rr := httptest.NewRecorder()
				srv.Routes().ServeHTTP(rr, req)
				if rr.Code != http.StatusOK {
					t.Fatalf("reservation failed: status=%d body=%s", rr.Code, rr.Body.String())
				}
				var expiryDays int
				if err := srv.db.QueryRow("SELECT expiry_days FROM uploads WHERE original_name = ?", filename).Scan(&expiryDays); err != nil {
					t.Fatal(err)
				}
				return expiryDays
			}

			if got := request("allowed.txt"); got != 0 {
				t.Fatalf("allowed keep-forever expiry_days=%d, want 0", got)
			}
			if _, err := srv.db.Exec("UPDATE people SET allow_user_keep_forever = 0 WHERE id = ?", personID); err != nil {
				t.Fatal(err)
			}
			if got := request("denied.txt"); got != 14 {
				t.Fatalf("denied keep-forever expiry_days=%d, want default 14", got)
			}
		})
	}
}

func TestDirectUploadPersistsKeepForeverOnlyWhenAllowed(t *testing.T) {
	srv, tempDir := setupTestServer(t)
	defer os.RemoveAll(tempDir)
	defer srv.db.Close()

	token := "direct_keep_forever_token"
	personID := insertTestUserAndSession(t, srv, token)
	if _, err := srv.db.Exec("UPDATE people SET allow_user_keep_forever = 1 WHERE id = ?", personID); err != nil {
		t.Fatal(err)
	}

	upload := func(filename string) (bool, sql.NullTime) {
		var body bytes.Buffer
		writer := multipart.NewWriter(&body)
		part, err := writer.CreateFormFile("file", filename)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := part.Write([]byte("test payload")); err != nil {
			t.Fatal(err)
		}
		if err := writer.WriteField("keep_forever", "true"); err != nil {
			t.Fatal(err)
		}
		if err := writer.Close(); err != nil {
			t.Fatal(err)
		}

		req := httptest.NewRequest(http.MethodPost, "/api/files/upload/direct", &body)
		req.Header.Set("Content-Type", writer.FormDataContentType())
		req.AddCookie(&http.Cookie{Name: "homeshare_session", Value: token})
		rr := httptest.NewRecorder()
		srv.Routes().ServeHTTP(rr, req)
		if rr.Code != http.StatusOK {
			t.Fatalf("direct upload failed: status=%d body=%s", rr.Code, rr.Body.String())
		}

		var keepForever bool
		var expiresAt sql.NullTime
		if err := srv.db.QueryRow("SELECT keep_forever, expires_at FROM files WHERE original_name = ?", filename).Scan(&keepForever, &expiresAt); err != nil {
			t.Fatal(err)
		}
		return keepForever, expiresAt
	}

	keepForever, expiresAt := upload("allowed-direct.txt")
	if !keepForever || expiresAt.Valid {
		t.Fatalf("allowed upload keep_forever=%v expires_at=%v, want true/NULL", keepForever, expiresAt)
	}

	if _, err := srv.db.Exec("UPDATE people SET allow_user_keep_forever = 0 WHERE id = ?", personID); err != nil {
		t.Fatal(err)
	}
	keepForever, expiresAt = upload("denied-direct.txt")
	if keepForever || !expiresAt.Valid {
		t.Fatalf("denied upload keep_forever=%v expires_at=%v, want false/non-NULL", keepForever, expiresAt)
	}
}

func TestAdminPeoplePersistsKeepForeverFlag(t *testing.T) {
	srv, tempDir := setupTestServer(t)
	defer os.RemoveAll(tempDir)
	defer srv.db.Close()

	token := "admin_people_token"
	insertTestAdminSession(t, srv, token)
	request := func(body string) httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodPost, "/api/admin/people/create", bytes.NewBufferString(body))
		req.Header.Set("Authorization", "Bearer "+token)
		req.Header.Set("Content-Type", "application/json")
		rr := httptest.NewRecorder()
		srv.Routes().ServeHTTP(rr, req)
		return *rr
	}

	created := request(`{"label":"Keep Forever User","allow_user_keep_forever":true}`)
	if created.Code != http.StatusOK {
		t.Fatalf("create failed: status=%d body=%s", created.Code, created.Body.String())
	}
	var response struct {
		ID int64 `json:"id"`
	}
	if err := json.Unmarshal(created.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	var allowed bool
	if err := srv.db.QueryRow("SELECT allow_user_keep_forever FROM people WHERE id = ?", response.ID).Scan(&allowed); err != nil {
		t.Fatal(err)
	}
	if !allowed {
		t.Fatal("created person did not persist allow_user_keep_forever=true")
	}

	updated := request(`{"id":` + fmt.Sprint(response.ID) + `,"label":"Keep Forever User","allow_user_keep_forever":false}`)
	if updated.Code != http.StatusOK {
		t.Fatalf("update failed: status=%d body=%s", updated.Code, updated.Body.String())
	}
	if err := srv.db.QueryRow("SELECT allow_user_keep_forever FROM people WHERE id = ?", response.ID).Scan(&allowed); err != nil {
		t.Fatal(err)
	}
	if allowed {
		t.Fatal("updated person did not persist allow_user_keep_forever=false")
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
