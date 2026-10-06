// Package config loads the application configuration from the environment and the .env file.
package config

import (
	"fmt"
	"log"
	"net"
	"os"
	"path/filepath"

	"github.com/ordaen/orgo/env"
)

var (
	// AppRootDir contains root of application
	AppRootDir string
	// ListenAddress contains listen address for webserver
	ListenAddress string
	// CacheFile contains path to cache file
	CacheFile string
	// LogDir contains path to logs
	LogDir string
	// LibDir contains path to libs
	LibDir string
	// CertDir contains path to certs
	CertDir string
	// CACert contains path to ca certificate
	CACert string
	// CAKey contains path to ca key
	CAKey string
	// CookieDomain contains cookie domain for portal authentication
	CookieDomain string
	// ExternalIP contains external IP address
	ExternalIP string
)

// Load initializes the config. AppRootDir is the parent of the directory of the executable.
// The .env file name is loaded when given, otherwise the .env file in AppRootDir when it exists.
// The environment variables already set are not changed by the .env file.
func Load(name string) error {
	exe, err := os.Executable()
	if err != nil {
		return fmt.Errorf("config: executable path: %w", err)
	}
	AppRootDir = filepath.Dir(filepath.Dir(exe))
	if name != "" {
		if err := env.Load(name); err != nil {
			return fmt.Errorf("config: load %s: %w", name, err)
		}
	} else if err := env.Load(filepath.Join(AppRootDir, ".env")); err == nil {
		log.Println("Loading environment variables from .env file")
	}
	// Load ENV variables
	ListenAddress = env.Get("LISTEN_ADDRESS", "0.0.0.0:8080")
	CookieDomain = env.Get("COOKIE_DOMAIN", "localhost")

	// Set global variables
	LibDir = filepath.Join(AppRootDir, "lib")
	CertDir = filepath.Join(LibDir, "certs")
	CACert = filepath.Join(CertDir, "ca.crt")
	CAKey = filepath.Join(CertDir, "ca.key")
	LogDir = filepath.Join(AppRootDir, "log")
	CacheFile = filepath.Join(LibDir, "cache.db")

	// the outbound IP is looked up only when it is not configured
	if ExternalIP = env.Get("EXTERNAL_RPC_IP", ""); ExternalIP == "" {
		ExternalIP = getExternalIP()
	}

	loadCustomVars()
	return nil
}

func getExternalIP() string {
	ip, err := GetOutboundIP()
	if err == nil {
		return ip.String()
	}
	return ""
}

// GetOutboundIP returns the local IP address of the default route. No packets are sent,
// connecting a UDP socket only selects the route.
func GetOutboundIP() (net.IP, error) {
	conn, err := net.Dial("udp", "8.8.8.8:80")
	if err != nil {
		return nil, err
	}
	defer conn.Close()
	addr := conn.LocalAddr().(*net.UDPAddr)
	return addr.IP, nil
}
