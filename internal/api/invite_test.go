package api

import (
	"path/filepath"
	"testing"
	"time"

	"lares/internal/db"
	"lares/internal/models"
)

func TestConsumeInviteActivationDisablesExhaustedInvite(t *testing.T) {
	database, err := db.InitDB(filepath.Join(t.TempDir(), "lares.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	_, err = database.Exec(`INSERT INTO invite_codes
		(id, person_id, code_hash, code_prefix, enabled, max_activations, activations_used, expires_at, created_at, created_by_admin_id)
		VALUES (1, 0, 'hash', 'CODE', 1, 1, 0, ?, ?, 0)`, time.Now().Add(time.Hour), time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if err := consumeInviteActivation(database, 1); err != nil {
		t.Fatal(err)
	}
	var enabled bool
	var used int
	if err := database.QueryRow("SELECT enabled, activations_used FROM invite_codes WHERE id = 1").Scan(&enabled, &used); err != nil {
		t.Fatal(err)
	}
	if enabled || used != 1 {
		t.Fatalf("enabled=%v used=%d, want false/1", enabled, used)
	}
	var active int
	if err := database.QueryRow("SELECT COUNT(*) FROM invite_codes WHERE enabled = 1 AND activations_used < max_activations").Scan(&active); err != nil {
		t.Fatal(err)
	}
	if active != 0 {
		t.Fatalf("exhausted invite still appears active: count=%d", active)
	}
	if err := consumeInviteActivation(database, 1); err == nil {
		t.Fatal("exhausted invite was consumed twice")
	}
}

func TestCanKeepForeverRequiresPersonPermission(t *testing.T) {
	allowed := &models.Person{AllowUserKeepForever: true}
	denied := &models.Person{AllowUserKeepForever: false}
	if !canKeepForever(allowed, true) {
		t.Fatal("allowed user could not request indefinite retention")
	}
	if canKeepForever(denied, true) || canKeepForever(nil, true) || canKeepForever(allowed, false) {
		t.Fatal("indefinite retention bypassed permission or explicit request")
	}
}

func TestResolveUploadRetentionFallsBackWhenPermissionIsMissing(t *testing.T) {
	allowed := &models.Person{AllowUserKeepForever: true}
	denied := &models.Person{AllowUserKeepForever: false}

	keepForever, expiryDays := resolveUploadRetention(allowed, true, 30, 14)
	if !keepForever || expiryDays != 0 {
		t.Fatalf("allowed retention=(%v, %d), want (true, 0)", keepForever, expiryDays)
	}
	keepForever, expiryDays = resolveUploadRetention(denied, true, 0, 14)
	if keepForever || expiryDays != 14 {
		t.Fatalf("denied retention=(%v, %d), want (false, 14)", keepForever, expiryDays)
	}
}

func TestAuditLocalization(t *testing.T) {
	if got := localizeAuditActor("person"); got != "Пользователь" {
		t.Fatalf("actor=%q", got)
	}
	if got := localizeAuditEvent("file_quarantined"); got != "Файл помещён в карантин" {
		t.Fatalf("event=%q", got)
	}
	if got := localizeAuditDetails("Downloaded 'report.pdf'"); got != "Скачан 'report.pdf'" {
		t.Fatalf("details=%q", got)
	}
}
