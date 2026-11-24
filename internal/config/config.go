// Package config reads Printty's settings from the environment and an
// optional printty.env file (KEY=VALUE lines). Secrets live only there.
package config

import (
	"bufio"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
)

type Config struct {
	Addr        string // listen address, default ":8631"
	DataDir     string // state.json and uploaded files
	PublicURL   string // base URL used in Telegram links, e.g. https://pc.tailnet.ts.net:8631
	TLSCert     string
	TLSKey      string
	TelegramBot string // bot token (never logged)
	TelegramIDs string // comma-separated admin chat IDs
	MaxUploadMB int
}

// Load reads env files then the environment (environment wins).
// Files checked: $PRINTTY_ENV, ./printty.env, <data dir>/printty.env.
func Load() Config {
	files := []string{os.Getenv("PRINTTY_ENV"), "printty.env"}
	vals := map[string]string{}
	for _, f := range files {
		readEnvFile(f, vals)
	}
	get := func(k, def string) string {
		if v, ok := os.LookupEnv(k); ok && v != "" {
			return v
		}
		if v := vals[k]; v != "" {
			return v
		}
		return def
	}
	c := Config{
		Addr:    get("PRINTTY_ADDR", ":8631"),
		DataDir: get("PRINTTY_DATA_DIR", defaultDataDir()),
	}
	// A printty.env inside the data dir is read last, for service installs.
	readEnvFile(filepath.Join(c.DataDir, "printty.env"), vals)
	c.PublicURL = get("PRINTTY_PUBLIC_URL", "")
	c.TLSCert = get("PRINTTY_TLS_CERT", "")
	c.TLSKey = get("PRINTTY_TLS_KEY", "")
	c.TelegramBot = get("TELEGRAM_BOT_TOKEN", "")
	c.TelegramIDs = get("TELEGRAM_CHAT_ID", "")
	c.MaxUploadMB, _ = strconv.Atoi(get("PRINTTY_MAX_UPLOAD_MB", "50"))
	if c.MaxUploadMB <= 0 {
		c.MaxUploadMB = 50
	}
	return c
}

func readEnvFile(path string, into map[string]string) {
	if path == "" {
		return
	}
	f, err := os.Open(path)
	if err != nil {
		return
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		k, v, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		v = strings.Trim(strings.TrimSpace(v), `"'`)
		into[strings.TrimSpace(k)] = v
	}
}

func defaultDataDir() string {
	switch runtime.GOOS {
	case "windows":
		if p := os.Getenv("ProgramData"); p != "" {
			return filepath.Join(p, "Printty")
		}
	case "darwin":
		if h, err := os.UserHomeDir(); err == nil {
			return filepath.Join(h, "Library", "Application Support", "Printty")
		}
	default:
		if os.Geteuid() == 0 {
			return "/var/lib/printty"
		}
		if h, err := os.UserHomeDir(); err == nil {
			return filepath.Join(h, ".local", "share", "printty")
		}
	}
	return "printty-data"
}
