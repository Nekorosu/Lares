package db

import (
	"database/sql"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestMigrationPreservesExistingRowsAndBackup(t *testing.T) {
	p := filepath.Join(t.TempDir(), "test.db")
	d, e := InitDB(p)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = d.Exec(`INSERT INTO people(id,label,storage_quota_bytes,monthly_upload_limit_bytes,monthly_download_limit_bytes,max_file_size_bytes,created_at) VALUES(1,'Admin',100,100,100,100,?);`, time.Now().UTC()); e != nil {
		t.Fatal(e)
	}
	if _, e = d.Exec(`INSERT INTO files(id,person_id,uploader_name,original_name,stored_path,size,content_type,status,created_at,client_ip_hash) VALUES('safe',1,'Admin','test','safe',3,'text/plain','ready',?,'')`, time.Now().UTC()); e != nil {
		t.Fatal(e)
	}
	d.Exec("PRAGMA user_version=0")
	d.Close()
	d, e = InitDB(p)
	if e != nil {
		t.Fatal(e)
	}
	defer d.Close()
	var owner int
	if e = d.QueryRow("SELECT person_id FROM files WHERE id='safe'").Scan(&owner); e != nil || owner != 1 {
		t.Fatal("name-based reassignment or data loss", e, owner)
	}
	if _, e = os.Stat(p + ".pre-v1.db"); e != nil {
		t.Fatal(e)
	}
	var v int
	d.QueryRow("PRAGMA user_version").Scan(&v)
	if v != 1 {
		t.Fatal(v)
	}
}
func TestUnknownLegacyStopsWithoutDataLoss(t *testing.T) {
	p := filepath.Join(t.TempDir(), "legacy.db")
	d, e := sql.Open("sqlite", p)
	if e != nil {
		t.Fatal(e)
	}
	d.Exec("CREATE TABLE persons(id INTEGER PRIMARY KEY,label TEXT); INSERT INTO persons VALUES(1,'Keep me')")
	d.Close()
	if d, e = InitDB(p); e == nil {
		d.Close()
		t.Fatal("unknown schema accepted")
	}
	d, _ = sql.Open("sqlite", p)
	defer d.Close()
	var label string
	if e = d.QueryRow("SELECT label FROM persons WHERE id=1").Scan(&label); e != nil || label != "Keep me" {
		t.Fatal("legacy lost", e)
	}
}

func TestUpgradeActualPreAuditSchema(t *testing.T) {
	p := filepath.Join(t.TempDir(), "old.db")
	d, e := sql.Open("sqlite", p)
	if e != nil {
		t.Fatal(e)
	}
	schema, e := os.ReadFile("testdata/pre_audit.sql")
	if e != nil {
		t.Fatal(e)
	}
	if _, e = d.Exec(string(schema)); e != nil {
		t.Fatal(e)
	}
	now := time.Now().UTC()
	if _, e = d.Exec(`INSERT INTO people(id,label,storage_quota_bytes,monthly_upload_limit_bytes,monthly_download_limit_bytes,max_file_size_bytes,created_at) VALUES(1,'Owner',100,100,100,100,?)`, now); e != nil {
		t.Fatal(e)
	}
	if _, e = d.Exec(`INSERT INTO uploads(id,person_id,session_id,upload_secret_hash,original_name,declared_size,received_bytes,status,reservation_expires_at,created_at,client_ip_hash) VALUES('old-upload',1,NULL,'hashed','test.txt',10,3,'uploading',?,?,'hash')`, now.Add(time.Hour), now); e != nil {
		t.Fatal(e)
	}
	d.Close()
	d, e = InitDB(p)
	if e != nil {
		t.Fatal(e)
	}
	defer d.Close()
	var n int64
	if e = d.QueryRow("SELECT external_bytes FROM upload_traffic WHERE upload_id='old-upload'").Scan(&n); e != nil || n != 3 {
		t.Fatal("old progress lost", n, e)
	}
	if e = d.QueryRow("SELECT received_bytes FROM uploads WHERE id='old-upload'").Scan(&n); e != nil || n != 3 {
		t.Fatal(n, e)
	}
}
