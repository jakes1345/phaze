// grant-supporter: one-shot CLI to grant (or revoke) the supporter badge
// directly in the SQLite DB. Use when someone donated via Buy Me a Coffee
// but didn't fill out the in-app form, or when you want to grant it manually.
//
// Run on Fly:
//
//	fly ssh console -a skype7-reborn -C \
//	  "/app/grant-supporter -username alice"
//
// Revoke:
//
//	fly ssh console -a skype7-reborn -C \
//	  "/app/grant-supporter -username alice -revoke"
package main

import (
	"database/sql"
	"errors"
	"flag"
	"fmt"
	"os"

	_ "modernc.org/sqlite"
)

func main() {
	dbPath := flag.String("db", envOr("DB_PATH", "/data/nexus.db"), "SQLite DB path")
	username := flag.String("username", "", "Phaze username to grant/revoke badge for (required)")
	revoke := flag.Bool("revoke", false, "Revoke the badge instead of granting it")
	flag.Parse()

	if *username == "" {
		fmt.Fprintln(os.Stderr, "grant-supporter: -username required")
		os.Exit(1)
	}

	db, err := sql.Open("sqlite", *dbPath)
	if err != nil {
		fatalf("open db: %v", err)
	}
	defer db.Close()
	db.Exec(`PRAGMA busy_timeout = 5000`)

	var exists int
	err = db.QueryRow(`SELECT 1 FROM users WHERE username = ?`, *username).Scan(&exists)
	if errors.Is(err, sql.ErrNoRows) {
		fatalf("user %q not found", *username)
	} else if err != nil {
		fatalf("db lookup: %v", err)
	}

	if *revoke {
		if _, err := db.Exec(`UPDATE users SET supporter = 0, supporter_since = NULL WHERE username = ?`, *username); err != nil {
			fatalf("revoke: %v", err)
		}
		fmt.Printf("revoked supporter badge from %q\n", *username)
	} else {
		if _, err := db.Exec(
			`UPDATE users SET supporter = 1, supporter_since = CURRENT_TIMESTAMP WHERE username = ?`, *username,
		); err != nil {
			fatalf("grant: %v", err)
		}
		// Also create a synthetic supporter_requests row so it shows up in history.
		db.Exec(
			`INSERT OR IGNORE INTO supporter_requests (username, name, email, status)
			 VALUES (?, ?, '', 'granted')`, *username, *username)
		fmt.Printf("granted supporter badge to %q\n", *username)
	}
}

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func fatalf(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "grant-supporter: "+format+"\n", args...)
	os.Exit(1)
}
