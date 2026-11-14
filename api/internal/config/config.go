// Package config reads the API configuration from environment variables only.
package config

import (
	"errors"
	"fmt"
	"net"
	"net/netip"
	"os"

	"tracker/api/internal/app"
)

// Config is the runtime configuration of the API server and CLI.
type Config struct {
	// ListenAddr is the TCP address the HTTP server binds to.
	ListenAddr string
	// DBPath is the path of the SQLite database file.
	DBPath string
	// TrustedProxy is the IP address of the reverse proxy (Caddy). Only
	// requests whose socket peer is this address may set X-Forwarded-For.
	// The zero value means no proxy is trusted.
	TrustedProxy netip.Addr
}

// Load reads the configuration from the environment.
func Load() (Config, error) {
	c := Config{
		ListenAddr: env("LISTEN_ADDR", ":8080"),
		DBPath:     env("DB_PATH", "/data/tracker.db"),
	}
	if _, _, err := net.SplitHostPort(c.ListenAddr); err != nil {
		return Config{}, fmt.Errorf("%sLISTEN_ADDR: %w", app.EnvPrefix, err)
	}
	if c.DBPath == "" {
		return Config{}, errors.New(app.EnvPrefix + "DB_PATH must not be empty")
	}
	if v := env("TRUSTED_PROXY", ""); v != "" {
		ip, err := netip.ParseAddr(v)
		if err != nil {
			return Config{}, fmt.Errorf("%sTRUSTED_PROXY must be a single IP address: %w", app.EnvPrefix, err)
		}
		c.TrustedProxy = ip.Unmap()
	}
	return c, nil
}

func env(key, def string) string {
	if v, ok := os.LookupEnv(app.EnvPrefix + key); ok {
		return v
	}
	return def
}
