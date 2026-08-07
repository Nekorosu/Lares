package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"lares/internal/api"
	"lares/internal/auth"
	"lares/internal/config"
	"lares/internal/db"
)


func main() {
	if len(os.Args) > 1 && os.Args[1] == "admin" {
		handleAdminCLI(os.Args[2:])
		return
	}

	// Default: Run Server
	cfg, err := config.LoadConfig("")
	if err != nil {
		log.Fatalf("Failed to load config: %v", err)
	}

	database, err := db.InitDB(cfg.DBPath)
	if err != nil {
		log.Fatalf("Failed to initialize database: %v", err)
	}
	defer database.Close()

	server, err := api.NewServer(cfg, database)
	if err != nil {
		log.Fatalf("Failed to initialize API server: %v", err)
	}

	httpServer := &http.Server{
		Addr:         cfg.Listen,
		Handler:      server.Routes(),
		ReadTimeout:  30 * time.Second,
		WriteTimeout: 60 * time.Minute, // Long timeout for large streaming uploads/downloads
		IdleTimeout:  60 * time.Second,
	}

	go func() {
		log.Printf("Starting Homeshare server on http://%s", cfg.Listen)
		if err := httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("HTTP server error: %v", err)
		}
	}()

	// Graceful shutdown
	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt, syscall.SIGTERM)
	<-stop

	log.Println("Shutting down Homeshare server gracefully...")
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := httpServer.Shutdown(ctx); err != nil {
		log.Printf("Error during shutdown: %v", err)
	}
	log.Println("Server stopped successfully.")
}

func handleAdminCLI(args []string) {
	if len(args) == 0 {
		printCLIUsage()
		return
	}

	cfg, err := config.LoadConfig("")
	if err != nil {
		log.Fatalf("Failed to load config: %v", err)
	}

	database, err := db.InitDB(cfg.DBPath)
	if err != nil {
		log.Fatalf("Failed to initialize database: %v", err)
	}
	defer database.Close()

	subcommand := args[0]
	subArgs := args[1:]

	switch subcommand {
	case "create":
		createCmd := flag.NewFlagSet("create", flag.ExitOnError)
		username := createCmd.String("username", "admin", "Admin username")
		password := createCmd.String("password", "", "Admin password")
		_ = createCmd.Parse(subArgs)

		if *password == "" {
			fmt.Print("Enter admin password (min 12 chars): ")
			fmt.Scanln(password)
		}

		if err := auth.ValidatePassword(*username, *password); err != nil {
			log.Fatalf("Password validation error: %v", err)
		}

		passHash, err := auth.HashPassword(*password)
		if err != nil {
			log.Fatalf("Failed to hash password: %v", err)
		}

		totpSecret, err := auth.GenerateTOTPSecret()
		if err != nil {
			log.Fatalf("Failed to generate TOTP secret: %v", err)
		}

		_, err = database.Exec(`
			INSERT INTO admin_users (username, password_hash, totp_secret, totp_enabled, created_at)
			VALUES (?, ?, ?, 1, ?)
		`, *username, passHash, totpSecret, time.Now().UTC())

		if err != nil {
			log.Fatalf("Failed to create admin user: %v", err)
		}

		fmt.Printf("\n[SUCCESS] Admin user '%s' created successfully!\n", *username)
		fmt.Printf("TOTP Secret: %s\n", totpSecret)

	case "delete":
		deleteCmd := flag.NewFlagSet("delete", flag.ExitOnError)
		username := deleteCmd.String("username", "", "Admin username to delete")
		_ = deleteCmd.Parse(subArgs)

		if *username == "" {
			log.Fatal("Error: --username is required")
		}

		res, err := database.Exec("DELETE FROM admin_users WHERE username = ?", *username)
		if err != nil {
			log.Fatalf("Failed to delete admin: %v", err)
		}

		rows, _ := res.RowsAffected()
		if rows == 0 {
			fmt.Printf("Admin user '%s' not found.\n", *username)
		} else {
			fmt.Printf("Admin user '%s' deleted successfully.\n", *username)
		}

	case "reset-totp":
		resetCmd := flag.NewFlagSet("reset-totp", flag.ExitOnError)
		username := resetCmd.String("username", "", "Admin username")
		_ = resetCmd.Parse(subArgs)

		if *username == "" {
			log.Fatal("Error: --username is required")
		}

		newSecret, err := auth.GenerateTOTPSecret()
		if err != nil {
			log.Fatalf("Failed to generate TOTP secret: %v", err)
		}

		res, err := database.Exec("UPDATE admin_users SET totp_secret = ?, totp_enabled = 1 WHERE username = ?", newSecret, *username)
		if err != nil {
			log.Fatalf("Failed to reset TOTP: %v", err)
		}

		rows, _ := res.RowsAffected()
		if rows == 0 {
			fmt.Printf("Admin user '%s' not found.\n", *username)
		} else {
			fmt.Printf("\n[SUCCESS] TOTP reset for admin '%s'\n", *username)
			fmt.Printf("New TOTP Secret: %s\n", newSecret)
		}

	case "unlock":
		unlockCmd := flag.NewFlagSet("unlock", flag.ExitOnError)
		username := unlockCmd.String("username", "", "Admin username")
		_ = unlockCmd.Parse(subArgs)

		if *username == "" {
			log.Fatal("Error: --username is required")
		}

		prefix := fmt.Sprintf("admin_lock_%s_", *username)
		_, err = database.Exec("DELETE FROM rate_limit_locks WHERE key LIKE ?", prefix+"%")
		if err != nil {
			log.Fatalf("Failed to unlock admin: %v", err)
		}

		fmt.Printf("Admin user '%s' unlocked successfully.\n", *username)

	default:
		printCLIUsage()
	}
}

func printCLIUsage() {
	fmt.Println("Usage:")
	fmt.Println("  homeshare serve                           Start the web server")
	fmt.Println("  homeshare admin create [--username admin] [--password pass]")
	fmt.Println("  homeshare admin delete --username admin")
	fmt.Println("  homeshare admin reset-totp --username admin")
	fmt.Println("  homeshare admin unlock --username admin")
}
