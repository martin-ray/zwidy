package config

import (
	"fmt"
	"strings"
)

func ParseYAML(input string) (*Config, error) {
	cfg := &Config{}
	lines := strings.Split(input, "\n")
	var section string
	var listSection string
	var currentClient *ClientConfig
	var currentService *ServiceConfig

	for index, raw := range lines {
		line := stripComment(strings.TrimRight(raw, " \t\r"))
		if strings.TrimSpace(line) == "" || strings.TrimSpace(line) == "---" {
			continue
		}
		indent := countIndent(line)
		trimmed := strings.TrimSpace(line)

		if indent == 0 {
			listSection = ""
			currentClient = nil
			currentService = nil
			if strings.HasSuffix(trimmed, ":") {
				section = strings.TrimSuffix(trimmed, ":")
				continue
			}
			key, val, ok := splitKV(trimmed)
			if !ok {
				return nil, fmt.Errorf("yaml line %d: expected key: value", index+1)
			}
			if err := assignRoot(cfg, key, val); err != nil {
				return nil, fmt.Errorf("yaml line %d: %w", index+1, err)
			}
			continue
		}

		if indent == 2 && strings.HasSuffix(trimmed, ":") {
			listSection = strings.TrimSuffix(trimmed, ":")
			continue
		}

		if indent == 2 && strings.HasPrefix(trimmed, "- ") {
			item := strings.TrimPrefix(trimmed, "- ")
			switch section {
			case "clients":
				cfg.Clients = append(cfg.Clients, ClientConfig{})
				currentClient = &cfg.Clients[len(cfg.Clients)-1]
				if item != "" {
					key, val, ok := splitKV(item)
					if !ok {
						return nil, fmt.Errorf("yaml line %d: expected client key: value", index+1)
					}
					assignClient(currentClient, key, val)
				}
			case "services":
				cfg.Services = append(cfg.Services, ServiceConfig{})
				currentService = &cfg.Services[len(cfg.Services)-1]
				if item != "" {
					key, val, ok := splitKV(item)
					if !ok {
						return nil, fmt.Errorf("yaml line %d: expected service key: value", index+1)
					}
					assignService(currentService, key, val)
				}
			default:
				return nil, fmt.Errorf("yaml line %d: unsupported list in section %q", index+1, section)
			}
			continue
		}

		key, val, ok := splitKV(trimmed)
		if !ok {
			return nil, fmt.Errorf("yaml line %d: expected key: value", index+1)
		}

		if currentClient != nil && indent >= 4 && section == "clients" {
			assignClient(currentClient, key, val)
			continue
		}
		if currentService != nil && indent >= 4 && section == "services" {
			assignService(currentService, key, val)
			continue
		}
		if err := assignSection(cfg, section, key, val); err != nil {
			if listSection != "" {
				if err2 := assignSection(cfg, listSection, key, val); err2 == nil {
					continue
				}
			}
			return nil, fmt.Errorf("yaml line %d: %w", index+1, err)
		}
	}
	applyDefaults(cfg)
	return cfg, nil
}

func stripComment(line string) string {
	if idx := strings.Index(line, "#"); idx >= 0 {
		return line[:idx]
	}
	return line
	}

func countIndent(line string) int {
	count := 0
	for _, r := range line {
		if r != ' ' {
			break
		}
		count++
	}
	return count
	}

func splitKV(line string) (string, string, bool) {
	parts := strings.SplitN(line, ":", 2)
	if len(parts) != 2 {
		return "", "", false
	}
	return strings.TrimSpace(parts[0]), unquote(strings.TrimSpace(parts[1])), true
	}

func unquote(s string) string {
	if len(s) >= 2 {
		if (s[0] == '\'' && s[len(s)-1] == '\'') || (s[0] == '"' && s[len(s)-1] == '"') {
			return s[1 : len(s)-1]
		}
	}
	return s
	}

func assignRoot(cfg *Config, key, val string) error {
	switch key {
	case "mode":
		cfg.Mode = val
		return nil
	default:
		return fmt.Errorf("unsupported root key %q", key)
	}
	}

func assignSection(cfg *Config, section, key, val string) error {
	switch section {
	case "node":
		if key == "id" {
			cfg.Node.ID = val
			return nil
		}
	case "listen":
		switch key {
		case "address":
			cfg.Listen.Address = val
		case "port":
			cfg.Listen.Port = parseInt(val)
		case "transport":
			cfg.Listen.Transport = val
		default:
			return fmt.Errorf("unsupported key listen.%s", key)
		}
		return nil
	case "server":
		switch key {
		case "address":
			cfg.Server.Address = val
		case "port":
			cfg.Server.Port = parseInt(val)
		case "transport":
			cfg.Server.Transport = val
		default:
			return fmt.Errorf("unsupported key server.%s", key)
		}
		return nil
	case "network":
		switch key {
		case "interface":
			cfg.Network.Interface = val
		case "cidr":
			cfg.Network.CIDR = val
		case "server_ip":
			cfg.Network.ServerIP = val
		case "mtu":
			cfg.Network.MTU = parseInt(val)
		case "client_to_client":
			cfg.Network.ClientToClient = parseBool(val)
		case "max_packet_size":
			cfg.Network.MaxPacketSize = parseInt(val)
		default:
			return fmt.Errorf("unsupported key network.%s", key)
		}
		return nil
	case "ipam":
		if key == "database" {
			cfg.IPAM.Database = val
			return nil
		}
	case "tls":
		switch key {
		case "certificate":
			cfg.TLS.Certificate = val
		case "private_key":
			cfg.TLS.PrivateKey = val
		case "ca":
			cfg.TLS.CA = val
		case "server_name":
			cfg.TLS.ServerName = val
		default:
			return fmt.Errorf("unsupported key tls.%s", key)
		}
		return nil
	case "auth":
		switch key {
		case "credential_file":
			cfg.Auth.CredentialFile = val
		case "private_key_file":
			cfg.Auth.PrivateKeyFile = val
		case "token":
			cfg.Auth.Token = val
		default:
			return fmt.Errorf("unsupported key auth.%s", key)
		}
		return nil
	case "logging":
		switch key {
		case "level":
			cfg.Logging.Level = val
		case "format":
			cfg.Logging.Format = val
		case "output":
			cfg.Logging.Output = val
		default:
			return fmt.Errorf("unsupported key logging.%s", key)
		}
		return nil
	case "reconnect":
		switch key {
		case "enabled":
			cfg.Reconnect.Enabled = parseBool(val)
		case "min_delay":
			cfg.Reconnect.MinDelay = parseDuration(val)
		case "max_delay":
			cfg.Reconnect.MaxDelay = parseDuration(val)
		default:
			return fmt.Errorf("unsupported key reconnect.%s", key)
		}
		return nil
	case "keepalive":
		switch key {
		case "interval":
			cfg.Keepalive.Interval = parseDuration(val)
		case "timeout":
			cfg.Keepalive.Timeout = parseDuration(val)
		default:
			return fmt.Errorf("unsupported key keepalive.%s", key)
		}
		return nil
	case "metrics":
		switch key {
		case "address":
			cfg.Metrics.Address = val
		case "enabled":
			cfg.Metrics.Enabled = parseBool(val)
		default:
			return fmt.Errorf("unsupported key metrics.%s", key)
		}
		return nil
	}
	return fmt.Errorf("unsupported section %q", section)
	}

func assignClient(client *ClientConfig, key, val string) {
	switch key {
	case "node_id":
		client.NodeID = val
	case "ip":
		client.IP = val
	case "token":
		client.Token = val
	}
	}

func assignService(service *ServiceConfig, key, val string) {
	switch key {
	case "name":
		service.Name = val
	case "listen":
		service.Listen = val
	case "target":
		service.Target = val
	}
	}
