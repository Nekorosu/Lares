package main

import (
	"bufio"
	"context"
	"flag"
	"fmt"
	"github.com/skip2/go-qrcode"
	"io"
	"lares/internal/api"
	"lares/internal/audit"
	"lares/internal/auth"
	"lares/internal/config"
	"lares/internal/db"
	"log"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"path/filepath"
	"regexp"
	"strings"
	"syscall"
	"time"
	"unsafe"
)

func main() {
	if e := run(os.Args[1:]); e != nil {
		log.Print(e)
		os.Exit(1)
	}
}
func run(args []string) error {
	if len(args) > 1 && args[0] == "config" && args[1] == "init" {
		f := flag.NewFlagSet("config init", flag.ContinueOnError)
		path := f.String("path", config.DefaultPath, "путь конфигурации")
		if e := f.Parse(args[2:]); e != nil {
			return e
		}
		if e := os.MkdirAll(filepath.Dir(*path), 0750); e != nil {
			return e
		}
		if e := config.Initialize(*path); e != nil {
			return e
		}
		fmt.Println("Конфигурация создана с постоянными секретами:", *path)
		return nil
	}
	if (len(args) == 0 || args[0] == "serve") && os.Geteuid() == 0 {
		return fmt.Errorf("сервер нельзя запускать от root; используйте пользователя homeshare")
	}
	cfg, e := config.LoadConfig("")
	if e != nil {
		return e
	}
	if len(args) > 0 && args[0] == "backup" {
		dest := filepath.Join(cfg.BackupDir, "upgrade-"+time.Now().UTC().Format("20060102T150405.000000000")+".db")
		if e := db.Backup(cfg.DBPath, dest); e != nil {
			return e
		}
		fmt.Println("Резервная копия:", dest)
		return nil
	}
	database, e := db.InitDB(cfg.DBPath)
	if e != nil {
		return e
	}
	defer database.Close()
	if len(args) > 0 && args[0] == "admin" {
		if len(args) < 2 {
			return fmt.Errorf("admin create|delete|reset-totp|unlock --username ИМЯ")
		}
		f := flag.NewFlagSet("admin", flag.ContinueOnError)
		username := f.String("username", "", "имя администратора")
		if e = f.Parse(args[2:]); e != nil {
			return e
		}
		if !regexp.MustCompile(`^[A-Za-z0-9_.-]{1,64}$`).MatchString(*username) {
			return fmt.Errorf("имя: 1..64 латинских букв, цифр, _, . или -")
		}
		switch args[1] {
		case "create":
			fmt.Print("Пароль (12..256 символов): ")
			password, e := readPassword()
			fmt.Println()
			if e != nil {
				return e
			}
			if e = auth.ValidatePassword(*username, password); e != nil {
				return e
			}
			hash, e := auth.HashPassword(password)
			if e != nil {
				return e
			}
			secret, e := auth.GenerateTOTPSecret()
			if e != nil {
				return e
			}
			if _, e = database.Exec("INSERT INTO admin_users(username,password_hash,totp_secret,totp_enabled,created_at) VALUES(?,?,?,1,?)", *username, hash, secret, time.Now().UTC()); e != nil {
				return e
			}
			if e = audit.NewLogger(database, cfg.Secrets.IPHashSalt).Log("system", 0, "admin_"+args[1], "admin", *username, "", "Команда CLI выполнена"); e != nil {
				return e
			}
			return showTOTP(*username, secret)
		case "delete":
			res, e := database.Exec("DELETE FROM admin_users WHERE username=?", *username)
			if e != nil {
				return e
			}
			n, _ := res.RowsAffected()
			if n != 1 {
				return fmt.Errorf("администратор не найден")
			}
			fmt.Println("Администратор и его сессии удалены")
		case "reset-totp":
			secret, e := auth.GenerateTOTPSecret()
			if e != nil {
				return e
			}
			tx, e := database.Begin()
			if e != nil {
				return e
			}
			defer tx.Rollback()
			res, e := tx.Exec("UPDATE admin_users SET totp_secret=?,totp_enabled=1,last_totp_step=0 WHERE username=?", secret, *username)
			if e != nil {
				return e
			}
			n, _ := res.RowsAffected()
			if n != 1 {
				return fmt.Errorf("администратор не найден")
			}
			if _, e = tx.Exec("DELETE FROM device_sessions WHERE admin_id IN (SELECT id FROM admin_users WHERE username=?)", *username); e != nil {
				return e
			}
			if e = tx.Commit(); e != nil {
				return e
			}
			if e = audit.NewLogger(database, cfg.Secrets.IPHashSalt).Log("system", 0, "admin_"+args[1], "admin", *username, "", "Команда CLI выполнена"); e != nil {
				return e
			}
			return showTOTP(*username, secret)
		case "unlock":
			prefix := "admin:" + *username + ":"
			tx, e := database.Begin()
			if e != nil {
				return e
			}
			defer tx.Rollback()
			for _, table := range []string{"rate_limit_locks", "request_events"} {
				if _, e = tx.Exec("DELETE FROM "+table+" WHERE substr(key,1,?)=?", len(prefix), prefix); e != nil {
					return e
				}
			}
			if e = tx.Commit(); e != nil {
				return e
			}
			fmt.Println("Блокировки и счётчики попыток сброшены")
		default:
			return fmt.Errorf("неизвестная команда admin")
		}
		return audit.NewLogger(database, cfg.Secrets.IPHashSalt).Log("system", 0, "admin_"+args[1], "admin", *username, "", "Команда CLI выполнена")
	}
	if len(args) > 0 && args[0] != "serve" {
		return fmt.Errorf("команды: serve, config init, admin create|delete|reset-totp|unlock")
	}
	if os.Geteuid() == 0 {
		return fmt.Errorf("сервер нельзя запускать от root; используйте пользователя homeshare")
	}
	lock, e := os.OpenFile(filepath.Join(filepath.Dir(cfg.DBPath), "serve.lock"), os.O_CREATE|os.O_RDWR, 0600)
	if e != nil {
		return e
	}
	defer lock.Close()
	if e = syscall.Flock(int(lock.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); e != nil {
		return fmt.Errorf("экземпляр сервера уже работает")
	}
	server, e := api.NewServer(cfg, database)
	if e != nil {
		return e
	}
	defer server.Close()
	hs := &http.Server{Addr: cfg.Listen, Handler: server.Routes(), ReadHeaderTimeout: 10 * time.Second, IdleTimeout: 60 * time.Second, MaxHeaderBytes: 64 << 10}
	errCh := make(chan error, 1)
	go func() { errCh <- hs.ListenAndServe() }()
	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt, syscall.SIGTERM)
	defer signal.Stop(stop)
	select {
	case e = <-errCh:
		if e != http.ErrServerClosed {
			return e
		}
	case <-stop:
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		if e = hs.Shutdown(ctx); e != nil {
			hs.Close()
			return e
		}
	}
	return nil
}
func readPassword() (string, error) {
	fd := os.Stdin.Fd()
	var old syscall.Termios
	_, _, errno := syscall.Syscall(syscall.SYS_IOCTL, fd, syscall.TCGETS, uintptr(unsafe.Pointer(&old)))
	if errno == 0 {
		next := old
		next.Lflag &^= syscall.ECHO
		_, _, errno = syscall.Syscall(syscall.SYS_IOCTL, fd, syscall.TCSETS, uintptr(unsafe.Pointer(&next)))
		if errno != 0 {
			return "", errno
		}
		defer syscall.Syscall(syscall.SYS_IOCTL, fd, syscall.TCSETS, uintptr(unsafe.Pointer(&old)))
	}
	line, e := bufio.NewReader(io.LimitReader(os.Stdin, 4097)).ReadString('\n')
	if e != nil {
		return "", e
	}
	return strings.TrimSuffix(strings.TrimSuffix(line, "\n"), "\r"), nil
}
func showTOTP(username, secret string) error {
	uri := "otpauth://totp/Lares:" + url.PathEscape(username) + "?secret=" + secret + "&issuer=Lares"
	qr, e := qrcode.New(uri, qrcode.Medium)
	if e != nil {
		return e
	}
	fmt.Println("Добавьте TOTP в приложение-аутентификатор. Секрет показывается только сейчас:")
	fmt.Println(secret)
	fmt.Println(qr.ToSmallString(false))
	return nil
}
