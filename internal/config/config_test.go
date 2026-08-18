package config

import "testing"

func TestParseAndValidateServerConfig(t *testing.T) {
	cfg, err := ParseYAML(`mode: server
listen:
  address: 0.0.0.0
  port: 51820
  transport: tcp
network:
  interface: zwidy0
  cidr: 10.77.0.0/24
  server_ip: 10.77.0.1
  mtu: 1380
  client_to_client: false
ipam:
  database: /tmp/zwidy.db
tls:
  certificate: /tmp/server.crt
  private_key: /tmp/server.key
logging:
  level: info
  format: json
  output: stdout
keepalive:
  interval: 15s
  timeout: 45s
clients:
  - node_id: node-a
    ip: 10.77.0.10
    token: secret
`)
	if err != nil {
		t.Fatal(err)
	}
	if err := cfg.Validate(); err != nil {
		t.Fatal(err)
	}
	if cfg.Clients[0].NodeID != "node-a" {
		t.Fatalf("unexpected node_id: %q", cfg.Clients[0].NodeID)
	}
}
