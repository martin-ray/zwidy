package config

import (
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

type Config struct {
	Mode      string
	Node      NodeConfig
	Listen    ListenConfig
	Server    ServerConfig
	Network   NetworkConfig
	IPAM      IPAMConfig
	TLS       TLSConfig
	Auth      AuthConfig
	Logging   LoggingConfig
	Reconnect ReconnectConfig
	Keepalive KeepaliveConfig
	Metrics   MetricsConfig
	Clients   []ClientConfig
	Services  []ServiceConfig
	Source    string
}

type NodeConfig struct{ ID string }

type ListenConfig struct {
	Address   string
	Port      int
	Transport string
}

type ServerConfig struct {
	Address   string
	Port      int
	Transport string
}

type NetworkConfig struct {
	Interface      string
	CIDR           string
	ServerIP       string
	MTU            int
	ClientToClient bool
	MaxPacketSize  int
}

type IPAMConfig struct{ Database string }

type TLSConfig struct {
	Certificate string
	PrivateKey  string
	CA          string
	ServerName  string
}

type AuthConfig struct {
	CredentialFile string
	PrivateKeyFile string
	Token          string
}

type LoggingConfig struct {
	Level  string
	Format string
	Output string
}

type ReconnectConfig struct {
	Enabled  bool
	MinDelay time.Duration
	MaxDelay time.Duration
}

type KeepaliveConfig struct {
	Interval time.Duration
	Timeout  time.Duration
}

type MetricsConfig struct {
	Address string
	Enabled bool
}

type ClientConfig struct {
	NodeID string
	IP     string
	Token  string
}

type ServiceConfig struct {
	Name   string
	Listen string
	Target string
}

func LoadFile(path string) (*Config, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read config: %w", err)
	}
	cfg, err := ParseYAML(string(b))
	if err != nil {
		return nil, err
	}
	cfg.Source = path
	applyDefaults(cfg)
	if err := cfg.Validate(); err != nil {
		return nil, fmt.Errorf("invalid configuration:\n%w", err)
	}
	return cfg, nil
}

func applyDefaults(cfg *Config) {
	if cfg.Listen.Address == "" {
		cfg.Listen.Address = "0.0.0.0"
	}
	if cfg.Listen.Port == 0 {
		cfg.Listen.Port = 51820
	}
	if cfg.Listen.Transport == "" {
		cfg.Listen.Transport = "quic"
	}
	if cfg.Server.Port == 0 {
		cfg.Server.Port = 51820
	}
	if cfg.Server.Transport == "" {
		cfg.Server.Transport = "quic"
	}
	if cfg.Network.Interface == "" {
		cfg.Network.Interface = "zwidy0"
	}
	if cfg.Network.MTU == 0 {
		cfg.Network.MTU = 1380
	}
	if cfg.Network.MaxPacketSize == 0 {
		cfg.Network.MaxPacketSize = 65535
	}
	if cfg.Logging.Level == "" {
		cfg.Logging.Level = "info"
	}
	if cfg.Logging.Format == "" {
		cfg.Logging.Format = "json"
	}
	if cfg.Logging.Output == "" {
		cfg.Logging.Output = "stdout"
	}
	if cfg.Reconnect.MinDelay == 0 {
		cfg.Reconnect.MinDelay = time.Second
	}
	if cfg.Reconnect.MaxDelay == 0 {
		cfg.Reconnect.MaxDelay = 60 * time.Second
	}
	if cfg.Keepalive.Interval == 0 {
		cfg.Keepalive.Interval = 15 * time.Second
	}
	if cfg.Keepalive.Timeout == 0 {
		cfg.Keepalive.Timeout = 45 * time.Second
	}
	if cfg.Metrics.Address == "" {
		cfg.Metrics.Address = "127.0.0.1:9090"
	}
	if !cfg.Metrics.Enabled {
		cfg.Metrics.Enabled = true
	}
	if cfg.Mode == "server" && cfg.IPAM.Database == "" {
		cfg.IPAM.Database = "/var/lib/zwidy/zwidy.db"
	}
	if cfg.TLS.ServerName == "" {
		cfg.TLS.ServerName = cfg.Server.Address
	}
}

func (c *Config) Validate() error {
	var errs []string
	if c.Mode != "server" && c.Mode != "client" {
		errs = append(errs, fmt.Sprintf("mode: expected server or client, got %q", c.Mode))
	}
	if c.Mode == "server" {
		if c.Network.CIDR == "" {
			errs = append(errs, "network.cidr: required")
		} else if _, _, err := net.ParseCIDR(c.Network.CIDR); err != nil {
			errs = append(errs, fmt.Sprintf("network.cidr: expected CIDR, got %q", c.Network.CIDR))
		}
		if ip := net.ParseIP(c.Network.ServerIP); ip == nil {
			errs = append(errs, fmt.Sprintf("network.server_ip: expected IP, got %q", c.Network.ServerIP))
		}
		if c.Listen.Transport != "tcp" && c.Listen.Transport != "quic" {
			errs = append(errs, fmt.Sprintf("listen.transport: expected tcp or quic, got %q", c.Listen.Transport))
		}
		if c.Listen.Port <= 0 || c.Listen.Port > 65535 {
			errs = append(errs, fmt.Sprintf("listen.port: invalid port %d", c.Listen.Port))
		}
		if c.TLS.Certificate == "" {
			errs = append(errs, "tls.certificate: required")
		}
		if c.TLS.PrivateKey == "" {
			errs = append(errs, "tls.private_key: required")
		}
	}
	if c.Mode == "client" {
		if c.Node.ID == "" {
			errs = append(errs, "node.id: required")
		}
		if c.Server.Address == "" {
			errs = append(errs, "server.address: required")
		}
		if c.Server.Transport != "tcp" && c.Server.Transport != "quic" {
			errs = append(errs, fmt.Sprintf("server.transport: expected tcp or quic, got %q", c.Server.Transport))
		}
		if c.Server.Port <= 0 || c.Server.Port > 65535 {
			errs = append(errs, fmt.Sprintf("server.port: invalid port %d", c.Server.Port))
		}
		if c.Auth.Token == "" && c.Auth.CredentialFile == "" {
			errs = append(errs, "auth.token or auth.credential_file: required")
		}
	}
	if c.Network.MTU < 576 {
		errs = append(errs, fmt.Sprintf("network.mtu: too small %d", c.Network.MTU))
	}
	if c.Network.MaxPacketSize < 1200 {
		errs = append(errs, fmt.Sprintf("network.max_packet_size: too small %d", c.Network.MaxPacketSize))
	}
	if c.Logging.Level != "debug" && c.Logging.Level != "info" && c.Logging.Level != "warn" && c.Logging.Level != "error" {
		errs = append(errs, fmt.Sprintf("logging.level: invalid value %q", c.Logging.Level))
	}
	if c.Logging.Format != "json" && c.Logging.Format != "text" {
		errs = append(errs, fmt.Sprintf("logging.format: invalid value %q", c.Logging.Format))
	}
	if c.Keepalive.Timeout <= c.Keepalive.Interval {
		errs = append(errs, fmt.Sprintf("keepalive.timeout: must be greater than keepalive.interval (%s <= %s)", c.Keepalive.Timeout, c.Keepalive.Interval))
	}
	if c.Reconnect.MaxDelay < c.Reconnect.MinDelay {
		errs = append(errs, fmt.Sprintf("reconnect.max_delay: must be >= reconnect.min_delay (%s < %s)", c.Reconnect.MaxDelay, c.Reconnect.MinDelay))
	}
	if len(errs) > 0 {
		return fmt.Errorf(strings.Join(errs, "\n"))
	}
	return nil
	}

func expandPath(path string) string {
	if path == "" || path == "stdout" || path == "stderr" {
		return path
	}
	if strings.HasPrefix(path, "~/") {
		if home, err := os.UserHomeDir(); err == nil {
			return filepath.Join(home, strings.TrimPrefix(path, "~/"))
		}
	}
	return path
	}

func parseBool(s string) bool {
	s = strings.TrimSpace(strings.ToLower(s))
	return s == "true" || s == "yes" || s == "1"
	}

func parseInt(s string) int {
	v, _ := strconv.Atoi(strings.TrimSpace(s))
	return v
	}

func parseDuration(s string) time.Duration {
	v, _ := time.ParseDuration(strings.TrimSpace(s))
	return v
	}
