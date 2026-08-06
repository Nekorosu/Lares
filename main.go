package main

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
)

type Config struct {
	Port         int    `json:"port"`
	Host         string `json:"host"`
	UploadDir    string `json:"upload_dir"`
	MaxFileSize  int64  `json:"max_file_size"`
	StorageQuota int64  `json:"storage_quota"`
}

type FileRecord struct {
	ID            string    `json:"id"`
	OriginalName  string    `json:"original_name"`
	StoredPath    string    `json:"stored_path"`
	Size          int64     `json:"size"`
	Status        string    `json:"status"`
	Flagged       bool      `json:"flagged"`
	FlagReason    string    `json:"flag_reason,omitempty"`
	CreatedAt     time.Time `json:"created_at"`
	ExpiresAt     time.Time `json:"expires_at"`
	UploaderLabel string    `json:"uploader_label"`
}

type InviteRecord struct {
	Code           string    `json:"code"`
	CreatedAt      time.Time `json:"created_at"`
	MaxActivations int       `json:"max_activations"`
	Activations    int       `json:"activations"`
	Revoked        bool      `json:"revoked"`
}

type SessionRecord struct {
	ID        int       `json:"id"`
	Device    string    `json:"device"`
	IP        string    `json:"ip"`
	LastSeen  time.Time `json:"last_seen"`
	Status    string    `json:"status"`
	Revoked   bool      `json:"revoked"`
}

type ServerState struct {
	mu        sync.RWMutex
	Files     []FileRecord     `json:"files"`
	Invites   []InviteRecord   `json:"invites"`
	Sessions  []SessionRecord  `json:"sessions"`
	Config    Config           `json:"config"`
}

var state = ServerState{
	Config: Config{
		Port:         3000,
		Host:         "0.0.0.0",
		UploadDir:    "./data/uploads",
		MaxFileSize:  5368709120,  // 5 GB
		StorageQuota: 53687091200, // 50 GB
	},
	Files: []FileRecord{
		{
			ID:            "file-init-001",
			OriginalName:  "lares-architecture-v1.pdf",
			StoredPath:    "lares-architecture-v1.pdf",
			Size:          4194304,
			Status:        "ready",
			Flagged:       false,
			CreatedAt:     time.Now().Add(-48 * time.Hour),
			ExpiresAt:     time.Now().Add(12 * 24 * time.Hour),
			UploaderLabel: "Администратор",
		},
	},
	Invites: []InviteRecord{
		{
			Code:           "LARE-8F92-4A1B-0001",
			CreatedAt:      time.Now().Add(-72 * time.Hour),
			MaxActivations: 10,
			Activations:    3,
			Revoked:        false,
		},
	},
	Sessions: []SessionRecord{
		{
			ID:       1,
			Device:   "Ubuntu 24.04 LTS Server (Daemon)",
			IP:       "127.0.0.1",
			LastSeen: time.Now(),
			Status:   "Активна",
			Revoked:  false,
		},
	},
}

func randomHex(n int) string {
	bytes := make([]byte, n)
	if _, err := rand.Read(bytes); err != nil {
		return fmt.Sprintf("%d", time.Now().UnixNano())
	}
	return hex.EncodeToString(bytes)
}

func isAdmin(r *http.Request) bool {
	roleHeader := r.Header.Get("x-user-role")
	authHeader := r.Header.Get("Authorization")
	return roleHeader == "admin" || strings.Contains(authHeader, "admin")
}

func jsonResponse(w http.ResponseWriter, status int, data interface{}) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(data)
}

func errorResponse(w http.ResponseWriter, status int, message string) {
	jsonResponse(w, status, map[string]string{"error": message})
}

func main() {
	uploadDir := state.Config.UploadDir
	if err := os.MkdirAll(uploadDir, 0755); err != nil {
		log.Printf("Warning: failed to create upload dir: %v", err)
	}

	mux := http.NewServeMux()

	// Auth Login
	mux.HandleFunc("/api/auth/login", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			errorResponse(w, http.StatusMethodNotAllowed, "Method not allowed")
			return
		}
		var req struct {
			Password string `json:"password"`
		}
		json.NewDecoder(r.Body).Decode(&req)
		if req.Password == "admin" || req.Password == "admin123" || req.Password == "123456" {
			jsonResponse(w, http.StatusOK, map[string]string{
				"role":     "admin",
				"username": "Администратор",
				"token":    "admin-session-token-" + randomHex(8),
			})
		} else {
			errorResponse(w, http.StatusUnauthorized, "Неверный пароль администратора. Используйте: admin")
		}
	})

	// Health
	mux.HandleFunc("/api/health", func(w http.ResponseWriter, r *http.Request) {
		jsonResponse(w, http.StatusOK, map[string]string{
			"status":  "ok",
			"service": "lares",
			"time":    time.Now().Format(time.RFC3339),
		})
	})

	// Stats
	mux.HandleFunc("/api/stats", func(w http.ResponseWriter, r *http.Request) {
		state.mu.RLock()
		defer state.mu.RUnlock()

		var usedStorage int64
		for _, f := range state.Files {
			usedStorage += f.Size
		}

		jsonResponse(w, http.StatusOK, map[string]interface{}{
			"service":          "lares",
			"version":          "1.24.0",
			"uptime":           "12d 4h 18m",
			"active_transfers": 1,
			"files_count":      len(state.Files),
			"storage_used":     usedStorage,
			"storage_total":    state.Config.StorageQuota,
			"max_file_size":    state.Config.MaxFileSize,
			"ratelimit_status": "норма (0 заблокировано)",
			"quarantine_count": 0,
		})
	})

	// Files List
	mux.HandleFunc("/api/files", func(w http.ResponseWriter, r *http.Request) {
		state.mu.RLock()
		defer state.mu.RUnlock()
		jsonResponse(w, http.StatusOK, state.Files)
	})

	// Direct Upload
	mux.HandleFunc("/api/files/upload/direct", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			errorResponse(w, http.StatusMethodNotAllowed, "Method not allowed")
			return
		}
		file, header, err := r.FormFile("file")
		if err != nil {
			errorResponse(w, http.StatusBadRequest, "No file uploaded")
			return
		}
		defer file.Close()

		fileID := "file-" + randomHex(6)
		storedPath := fileID + "_" + filepath.Base(header.Filename)
		dstPath := filepath.Join(uploadDir, storedPath)

		out, err := os.Create(dstPath)
		if err != nil {
			errorResponse(w, http.StatusInternalServerError, "Failed to save file")
			return
		}
		defer out.Close()

		written, err := io.Copy(out, file)
		if err != nil {
			errorResponse(w, http.StatusInternalServerError, "Failed to write file")
			return
		}

		userLabel := r.Header.Get("x-uploader-label")
		if userLabel == "" {
			if isAdmin(r) {
				userLabel = "Администратор"
			} else {
				userLabel = "Пользователь Web"
			}
		}

		record := FileRecord{
			ID:            fileID,
			OriginalName:  header.Filename,
			StoredPath:    storedPath,
			Size:          written,
			Status:        "ready",
			Flagged:       false,
			CreatedAt:     time.Now(),
			ExpiresAt:     time.Now().Add(14 * 24 * time.Hour),
			UploaderLabel: userLabel,
		}

		state.mu.Lock()
		state.Files = append([]FileRecord{record}, state.Files...)
		state.mu.Unlock()

		jsonResponse(w, http.StatusOK, record)
	})

	// Delete file
	mux.HandleFunc("/api/files/delete/", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodDelete {
			errorResponse(w, http.StatusMethodNotAllowed, "Method not allowed")
			return
		}
		fileID := strings.TrimPrefix(r.URL.Path, "/api/files/delete/")
		userRole := r.Header.Get("x-user-role")
		userLabel := r.Header.Get("x-uploader-label")

		state.mu.Lock()
		defer state.mu.Unlock()

		idx := -1
		for i, f := range state.Files {
			if f.ID == fileID {
				idx = i
				break
			}
		}
		if idx == -1 {
			errorResponse(w, http.StatusNotFound, "File not found")
			return
		}

		targetFile := state.Files[idx]
		if userRole != "admin" {
			if targetFile.UploaderLabel == "Администратор" || (targetFile.UploaderLabel != "" && targetFile.UploaderLabel != userLabel && targetFile.UploaderLabel != "Пользователь Web") {
				errorResponse(w, http.StatusForbidden, "Пользователям запрещено удалять файлы администратора.")
				return
			}
		}

		os.Remove(filepath.Join(uploadDir, targetFile.StoredPath))
		state.Files = append(state.Files[:idx], state.Files[idx+1:]...)
		jsonResponse(w, http.StatusOK, map[string]string{"message": "File deleted", "id": fileID})
	})

	// Admin Invites
	mux.HandleFunc("/api/admin/invites", func(w http.ResponseWriter, r *http.Request) {
		if !isAdmin(r) {
			errorResponse(w, http.StatusForbidden, "Доступ запрещен. Управление инвайтами доступно только администратору.")
			return
		}
		state.mu.RLock()
		defer state.mu.RUnlock()

		if r.Method == http.MethodGet {
			jsonResponse(w, http.StatusOK, state.Invites)
			return
		}
		if r.Method == http.MethodPost {
			code := fmt.Sprintf("LARE-%s-%s-%s", strings.ToUpper(randomHex(2)), strings.ToUpper(randomHex(2)), strings.ToUpper(randomHex(2)))
			inv := InviteRecord{
				Code:           code,
				CreatedAt:      time.Now(),
				MaxActivations: 5,
				Activations:    0,
				Revoked:        false,
			}
			state.mu.RUnlock()
			state.mu.Lock()
			state.Invites = append([]InviteRecord{inv}, state.Invites...)
			state.mu.Unlock()
			state.mu.RLock()
			jsonResponse(w, http.StatusOK, inv)
			return
		}
	})

	// Admin Quarantine Approve
	mux.HandleFunc("/api/admin/quarantine/", func(w http.ResponseWriter, r *http.Request) {
		if !isAdmin(r) {
			errorResponse(w, http.StatusForbidden, "Доступ запрещен. Одобрение карантина доступно только администратору.")
			return
		}
		parts := strings.Split(strings.TrimPrefix(r.URL.Path, "/api/admin/quarantine/"), "/")
		if len(parts) >= 2 && parts[1] == "approve" {
			fileID := parts[0]
			state.mu.Lock()
			defer state.mu.Unlock()
			for i, f := range state.Files {
				if f.ID == fileID {
					state.Files[i].Status = "ready"
					state.Files[i].Flagged = false
					state.Files[i].FlagReason = ""
					jsonResponse(w, http.StatusOK, map[string]string{"message": "File quarantine approved", "id": fileID})
					return
				}
			}
			errorResponse(w, http.StatusNotFound, "File not found")
			return
		}
	})

	// Admin Sessions
	mux.HandleFunc("/api/admin/sessions", func(w http.ResponseWriter, r *http.Request) {
		if !isAdmin(r) {
			errorResponse(w, http.StatusForbidden, "Доступ запрещен. Просмотр сессий доступен только администратору.")
			return
		}
		state.mu.RLock()
		defer state.mu.RUnlock()
		jsonResponse(w, http.StatusOK, state.Sessions)
	})

	// Revoke Session
	mux.HandleFunc("/api/admin/sessions/", func(w http.ResponseWriter, r *http.Request) {
		if !isAdmin(r) {
			errorResponse(w, http.StatusForbidden, "Доступ запрещен. Отзыв сессий доступен только администратору.")
			return
		}
		sessIDStr := strings.TrimPrefix(r.URL.Path, "/api/admin/sessions/")
		sessID, _ := strconv.Atoi(sessIDStr)

		state.mu.Lock()
		defer state.mu.Unlock()

		for i, s := range state.Sessions {
			if s.ID == sessID {
				state.Sessions[i].Revoked = true
				jsonResponse(w, http.StatusOK, map[string]interface{}{"message": "Session revoked", "session_id": sessID})
				return
			}
		}
		errorResponse(w, http.StatusNotFound, "Session not found")
	})

	// Static files fallback
	fs := http.FileServer(http.Dir("./dist"))
	mux.Handle("/", fs)

	port := 3000
	fmt.Printf("Lares Go Server listening on 0.0.0.0:%d\n", port)
	if err := http.ListenAndServe(fmt.Sprintf("0.0.0.0:%d", port), mux); err != nil {
		log.Fatalf("Server error: %v", err)
	}
}
