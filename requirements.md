# Zwidy v1 Technical Specification

## 1. Overview

**Zwidy** is a lightweight overlay network and reverse-origin connectivity daemon written in Go.

The primary use case is to allow servers located behind NAT, CGNAT, home routers, firewalls, or private networks to securely join a virtual network and become reachable from a central gateway without exposing inbound ports.

Typical use case:

```text
Internet
   |
 CDN Edge / NGINX
   |
zwidy server
   |
========================
     Zwidy Network
========================
   |
zwidy client
   |
Jellyfin / Origin
```

The daemon executable SHALL be named:

```bash
zwidyd
```

The same binary SHALL support both server and client modes.

```bash
zwidyd server --config /etc/zwidy/server.yaml
zwidyd client --config /etc/zwidy/client.yaml
```

Separate `zwidy-server` and `zwidy-client` binaries SHALL NOT be created in v1.

Reasons:

* Most protocol and tunnel code is shared.
* One release artifact is easier to distribute.
* Version compatibility is easier to maintain.
* systemd deployment is simpler.
* Future modes can be added without adding binaries.

---

# 2. Design Goals

Zwidy SHALL provide:

1. Secure client-to-server tunnel establishment.
2. Virtual IP allocation controlled by the server.
3. TUN-based Layer 3 networking.
4. Automatic routing configuration.
5. Persistent reconnecting tunnels.
6. Configurable client isolation.
7. TCP and UDP-based transport options.
8. Structured logging.
9. Configurable log destinations.
10. Persistent server-side IP allocation.
11. Authentication of clients.
12. Encryption of all tunnel traffic.
13. Graceful restart and shutdown.
14. Configuration entirely from YAML.
15. Operation as a long-running Linux daemon.

Zwidy SHOULD remain small enough that the architecture can be understood by one engineer.

---

# 3. Platform

v1 SHALL target:

```text
Linux amd64
Linux arm64
```

Linux is required because Zwidy will depend on:

```text
/dev/net/tun
routing tables
network interfaces
CAP_NET_ADMIN
```

macOS and Windows support are explicitly outside v1 scope.

---

# 4. Networking Architecture

Zwidy SHALL implement a Layer 3 overlay network using Linux TUN interfaces.

Example:

```text
Zwidy network:

10.77.0.0/24

Server:
10.77.0.1

Client A:
10.77.0.10

Client B:
10.77.0.11

Client C:
10.77.0.12
```

Each node creates a virtual interface:

```text
zwidy0
```

Example server:

```text
zwidy0
  inet 10.77.0.1/24
```

Example client:

```text
zwidy0
  inet 10.77.0.10/24
```

Packets sent to `zwidy0` SHALL be read by `zwidyd`, encapsulated into the tunnel transport, transmitted to the remote daemon, and written into the remote TUN interface.

---

# 5. Packet Flow

Example:

```text
NGINX
 |
 | GET http://10.77.0.10:8096
 |
Linux routing
 |
zwidy0
 |
zwidyd server
 |
encrypted tunnel
 |
zwidyd client
 |
zwidy0
 |
10.77.0.10
 |
Jellyfin :8096
```

The Linux kernel SHALL continue to handle normal IP routing.

Zwidy SHOULD avoid implementing an IP router in userspace beyond deciding which tunnel connection owns a given virtual IP.

---

# 6. Tunnel Topology

v1 SHALL use a hub-and-spoke topology.

```text
                  zwidy-server
                  10.77.0.1
                 /     |      \
                /      |       \
               /       |        \
          client A  client B  client C
          .10       .11       .12
```

Clients SHALL NOT establish tunnels directly with other clients.

Any client-to-client traffic SHALL pass through the server.

This simplifies:

* authentication
* routing
* access control
* NAT traversal
* logging
* observability
* IP management

---

# 7. Transport

Zwidy SHALL support two transport modes:

```yaml
transport: quic
```

and

```yaml
transport: tcp
```

## QUIC

`quic` SHALL be the recommended/default transport.

QUIC provides:

* UDP-based transport
* encryption through TLS
* multiplexing
* connection migration possibilities
* avoidance of TCP-over-TCP problems
* better behavior across lossy networks

Implementation SHOULD use a mature Go QUIC library such as `quic-go`.

Raw UDP SHALL NOT be implemented as a separate unreliable tunnel protocol in v1.

Therefore:

```text
transport: quic
```

means:

```text
Zwidy over QUIC over UDP
```

## TCP

TCP SHALL exist primarily as a compatibility/fallback mode for networks where UDP is blocked.

```text
Zwidy
 ↓
TLS
 ↓
TCP
```

TCP transport SHALL also be encrypted.

---

# 8. Default Ports

Suggested defaults:

```text
QUIC: UDP/51820
TCP:  TCP/51820
```

Both SHALL be configurable.

---

# 9. Encryption

All communication SHALL be encrypted.

Unencrypted operation SHALL NOT be supported.

QUIC mode SHALL use TLS 1.3 through QUIC.

TCP mode SHALL use TLS.

The server SHALL possess a server certificate.

Clients SHALL validate the server identity.

---

# 10. Client Authentication

Every client SHALL have a unique identity.

Example:

```text
node_id: osaka-origin-01
```

v1 authentication SHOULD use an enrollment model.

Initial enrollment:

```text
client
  |
  | node ID + enrollment token
  v
server
  |
  | validate token
  v
issue client credentials
```

After enrollment, the client SHOULD receive long-lived client credentials.

Preferred long-term authentication mechanism:

```text
mTLS client certificate
```

The enrollment token SHALL NOT be transmitted repeatedly after successful enrollment.

Future versions may support:

* OIDC
* short-lived certificates
* external PKI
* hardware-backed keys

---

# 11. Server IP Address Management

The server SHALL control virtual IP allocation.

Clients SHALL NOT choose arbitrary tunnel IP addresses by default.

Example configuration:

```yaml
network:
  cidr: 10.77.0.0/24
  server_ip: 10.77.0.1
```

When a client connects:

```text
client node_id
     |
     v
IPAM
     |
     v
10.77.0.10
```

The same node SHOULD receive the same address when reconnecting.

---

# 12. Persistent IPAM Database

Address allocation SHALL survive server restarts.

Suggested storage for v1:

```text
SQLite
```

Example records:

```text
node_id            virtual_ip
osaka-origin-01    10.77.0.10
tokyo-origin-01    10.77.0.11
home-jellyfin      10.77.0.12
```

SQLite is preferred over a custom flat-file database because it provides:

* transactional updates
* locking
* easy inspection
* simple backup
* mature Go libraries

---

# 13. Static IP Allocation

Server configuration MAY allow static mappings.

Example:

```yaml
clients:
  - node_id: jellyfin-home
    ip: 10.77.0.10

  - node_id: jellyfin-osaka
    ip: 10.77.0.11
```

If no explicit IP is configured, IPAM SHALL allocate one automatically.

---

# 14. Client Isolation

Server configuration SHALL support:

```yaml
network:
  client_to_client: false
```

When disabled:

```text
Client A → Server      allowed
Client B → Server      allowed

Client A → Client B    blocked
Client B → Client A    blocked
```

When enabled:

```yaml
network:
  client_to_client: true
```

the server SHALL route traffic between clients.

Client-to-client packets SHALL still traverse the server in v1.

---

# 15. Routing

Zwidy SHALL configure required routes automatically.

Example client:

```text
10.77.0.0/24 dev zwidy0
```

Example server:

```text
10.77.0.0/24 dev zwidy0
```

The daemon SHALL remove routes that it created when shutting down cleanly.

Zwidy SHALL NOT modify unrelated routes.

Zwidy SHALL record which routes/interfaces were created by Zwidy to allow safe cleanup.

---

# 16. Privileges

Zwidy requires networking privileges.

Recommended Linux capability:

```text
CAP_NET_ADMIN
```

The documentation SHALL discourage running the entire daemon as unrestricted root where possible.

v1 MAY initially run as root for simplicity.

Future versions SHOULD drop privileges after setting up the network interface.

---

# 17. MTU

MTU SHALL be configurable.

Example:

```yaml
network:
  mtu: 1380
```

Default:

```text
1380
```

The value should account for encapsulation overhead and reduce fragmentation problems.

---

# 18. Client Configuration

Example:

```yaml
mode: client

node:
  id: jellyfin-home

server:
  address: tunnel.example.com
  port: 51820
  transport: quic

network:
  interface: zwidy0
  mtu: 1380

auth:
  credential_file: /etc/zwidy/client.pem
  private_key_file: /etc/zwidy/client.key

logging:
  level: info
  format: json
  output: /var/log/zwidy/zwidyd.log

reconnect:
  enabled: true
  min_delay: 1s
  max_delay: 60s

keepalive:
  interval: 15s
  timeout: 45s
```

---

# 19. Server Configuration

Example:

```yaml
mode: server

listen:
  address: 0.0.0.0
  port: 51820
  transport: quic

network:
  interface: zwidy0
  cidr: 10.77.0.0/24
  server_ip: 10.77.0.1
  mtu: 1380
  client_to_client: false

ipam:
  database: /var/lib/zwidy/zwidy.db

tls:
  certificate: /etc/zwidy/server.crt
  private_key: /etc/zwidy/server.key

logging:
  level: info
  format: json
  output: /var/log/zwidy/zwidyd.log
```

---

# 20. Configuration Behavior

Configuration SHALL use YAML.

Default config locations:

```text
/etc/zwidy/server.yaml
/etc/zwidy/client.yaml
```

Explicit configuration SHALL be possible:

```bash
zwidyd server --config ./server.yaml
```

The daemon SHALL validate the configuration before creating network interfaces.

Configuration errors SHALL provide human-readable messages.

Example:

```text
invalid configuration:
network.cidr: expected CIDR, got "10.77.0.0"
```

---

# 21. Configuration Validation Command

The CLI SHOULD support:

```bash
zwidyd validate --config /etc/zwidy/server.yaml
```

Expected successful result:

```text
configuration valid
```

This SHALL NOT change networking state.

This command is useful for CI/CD and Ansible.

---

# 22. Logging

Zwidy SHALL support structured logs.

Formats:

```yaml
format: json
```

or

```yaml
format: text
```

Levels:

```text
debug
info
warn
error
```

Outputs:

```text
stdout
stderr
file
```

Example:

```yaml
logging:
  level: info
  format: json
  output: /var/log/zwidy/zwidyd.log
```

For container/systemd environments:

```yaml
logging:
  output: stdout
```

shall be supported.

---

# 23. Structured Log Fields

Logs SHOULD contain fields such as:

```json
{
  "timestamp": "...",
  "level": "info",
  "component": "tunnel",
  "node_id": "jellyfin-home",
  "virtual_ip": "10.77.0.10",
  "remote_address": "203.0.113.20:45281",
  "message": "client connected"
}
```

Important events SHALL include:

```text
daemon startup
daemon shutdown
client connection
client disconnection
authentication failure
IP allocation
IP release
tunnel reconnect
routing changes
TUN interface creation
transport errors
TLS errors
configuration errors
```

Packet-by-packet logging SHALL NOT be enabled by default.

---

# 24. Reconnection

Clients SHALL automatically reconnect.

Reconnect SHALL use exponential backoff with jitter.

Example:

```text
1s
2s
4s
8s
16s
...
60s maximum
```

Successful connection SHALL reset the backoff.

---

# 25. Keepalive

The transport SHALL detect dead peers.

Default:

```text
keepalive interval: 15 seconds
dead peer timeout: 45 seconds
```

Both SHALL be configurable.

---

# 26. Server Client Registry

The server SHALL maintain an in-memory registry.

Conceptually:

```go
type Client struct {
    NodeID       string
    VirtualIP    net.IP
    ConnectedAt  time.Time
    LastSeen     time.Time
    Transport    Transport
}
```

The server SHALL map:

```text
virtual IP → active tunnel connection
```

Example:

```text
10.77.0.10 → jellyfin-home connection
10.77.0.11 → osaka-origin connection
```

This mapping SHALL be used when forwarding packets from the server TUN device.

---

# 27. Packet Forwarding Model

Server:

```text
read packet from zwidy0
        |
parse destination IP
        |
lookup active client
        |
send packet through client's tunnel
```

Client:

```text
receive tunnel packet
        |
write packet to zwidy0
```

Reverse direction:

```text
read zwidy0
   |
send to server tunnel
   |
server writes to zwidy0
```

---

# 28. Security Rules

The server SHALL prevent a client from spoofing another client's tunnel IP.

For example:

Client A owns:

```text
10.77.0.10
```

A packet received from Client A with:

```text
source = 10.77.0.11
```

SHALL be rejected.

The server SHALL validate packet source addresses against the authenticated client identity.

This is mandatory.

---

# 29. Metrics

Zwidy SHOULD expose Prometheus metrics.

Default:

```text
127.0.0.1:9090/metrics
```

Suggested metrics:

```text
zwidy_connected_clients
zwidy_connections_total
zwidy_connection_errors_total

zwidy_bytes_received_total
zwidy_bytes_sent_total

zwidy_packets_received_total
zwidy_packets_sent_total
zwidy_packets_dropped_total

zwidy_auth_failures_total

zwidy_tunnel_latency_seconds

zwidy_client_connected
zwidy_client_reconnects_total
```

Labels MAY include:

```text
node_id
transport
```

High-cardinality labels such as source IP SHOULD be avoided.

---

# 30. Health Endpoint

The daemon SHOULD expose:

```text
/healthz
```

Example:

```json
{
  "status": "ok"
}
```

Server health MAY additionally expose:

```text
TUN initialized
IPAM available
listener active
```

---

# 31. Service Exposure

A future-friendly v1 configuration SHOULD allow local service mappings.

Example:

```yaml
services:
  - name: jellyfin
    listen: 10.77.0.10:8096
    target: 127.0.0.1:8096
```

This allows Jellyfin to remain bound only to localhost.

Conceptually:

```text
NGINX
 |
10.77.0.10:8096
 |
Zwidy Client
 |
127.0.0.1:8096
 |
Jellyfin
```

This feature MAY be implemented after basic L3 tunneling is functional.

It SHOULD NOT block the initial v1 milestone.

---

# 32. systemd

Expected service:

```text
zwidyd.service
```

Example command:

```text
ExecStart=/usr/local/bin/zwidyd client --config /etc/zwidy/client.yaml
```

The daemon SHALL correctly handle:

```text
SIGTERM
SIGINT
```

Shutdown SHALL:

```text
stop accepting traffic
close tunnel connections
remove Zwidy-created routes
remove Zwidy-created TUN interface
flush logs
exit cleanly
```

---

# 33. CLI

Minimum CLI:

```text
zwidyd server
zwidyd client
zwidyd validate
zwidyd version
```

Examples:

```bash
zwidyd server --config /etc/zwidy/server.yaml

zwidyd client --config /etc/zwidy/client.yaml

zwidyd validate --config /etc/zwidy/client.yaml

zwidyd version
```

---

# 34. Suggested Go Project Layout

```text
zwidy/
├── cmd/
│   └── zwidyd/
│       └── main.go
│
├── internal/
│   ├── config/
│   ├── daemon/
│   ├── tun/
│   ├── tunnel/
│   ├── transport/
│   │   ├── quic/
│   │   └── tcp/
│   ├── protocol/
│   ├── server/
│   ├── client/
│   ├── ipam/
│   ├── routing/
│   ├── auth/
│   ├── metrics/
│   └── logging/
│
├── configs/
│   ├── server.example.yaml
│   └── client.example.yaml
│
├── packaging/
│   └── systemd/
│       └── zwidyd.service
│
├── docs/
├── go.mod
└── README.md
```

---

# 35. Protocol Framing

Zwidy SHALL define an explicit application protocol instead of relying on arbitrary byte streams.

Message types SHOULD include:

```text
HELLO
AUTH
AUTH_OK
AUTH_ERROR

IP_ASSIGN
IP_RELEASE

DATA

PING
PONG

ERROR
DISCONNECT
```

Every connection SHALL begin with protocol negotiation.

Example:

```text
Client
  |
  | HELLO protocol=v1 node_id=foo
  v
Server
  |
  | AUTH challenge
  v
Client
  |
  | credentials
  v
Server
  |
  | AUTH_OK
  | IP_ASSIGN 10.77.0.10
  v
Client
```

Protocol versioning SHALL exist from the beginning.

Example:

```text
protocol_version = 1
```

---

# 36. Maximum Packet Size

Tunnel frames SHALL enforce a maximum frame size.

Packets larger than the configured maximum SHALL be rejected.

Malformed packets SHALL never cause daemon termination.

All protocol parsing SHALL treat remote input as untrusted.

---

# 37. Concurrency Model

The implementation SHOULD use Go goroutines.

Typical client:

```text
connection manager
        |
        +-- tunnel receive goroutine
        |
        +-- TUN receive goroutine
        |
        +-- keepalive goroutine
        |
        +-- metrics
```

Server:

```text
listener
 |
 +-- connection/session per client
 |
 +-- TUN reader
 |
 +-- client registry
 |
 +-- IPAM
 |
 +-- metrics
```

Unbounded goroutine creation SHALL be avoided.

---

# 38. Expected Deployment

Example CDN deployment:

```text
                       Internet
                           |
                         GSLB
                           |
             +-------------+-------------+
             |             |             |
          Tokyo ATS     Osaka ATS    Singapore ATS
             |             |             |
             +-------------+-------------+
                           |
                        NGINX
                           |
                     Zwidy Server
                           |
               ====================
                    Zwidy Network
               ====================
                    10.77.0.0/24
                           |
                      Zwidy Client
                    10.77.0.10
                           |
                       Jellyfin
```

The origin only requires outbound connectivity to the Zwidy server.

No inbound port forwarding SHALL be required on the origin network.

---

# 39. CI/CD Requirements

GitHub Actions SHOULD run:

```text
go fmt check
go vet
go test
go test -race
static analysis
build amd64
build arm64
configuration validation tests
```

Integration tests SHOULD eventually launch isolated Linux network namespaces to test:

```text
server
client A
client B
routing
client isolation
reconnect
IP allocation
```

---

# 40. Ansible Requirements

Ansible SHALL be capable of:

```text
installing zwidyd
installing configuration
creating directories
installing systemd unit
setting required capabilities
starting/restarting zwidyd
validating config before restart
```

Recommended deployment flow:

```text
upload new binary
      |
zwidyd validate
      |
config valid?
   /       \
 NO        YES
 |          |
abort     restart
```

---

# 41. Non-Goals for v1

The following SHALL NOT be required for initial v1:

```text
full WireGuard compatibility
BGP
mesh networking
IPv6
multi-server clustering
distributed IPAM
automatic NAT traversal between clients
mobile clients
Windows
macOS
kernel module
eBPF forwarding
GUI
web management panel
OIDC
multi-region server replication
```

These may be introduced later.

---

# 42. v1 Milestones

## Milestone 1 — Basic TUN

Create:

```text
zwidy0
```

on two Linux machines and transfer IP packets between them.

Success criterion:

```bash
ping 10.77.0.10
```

works across the tunnel.

## Milestone 2 — Server / Client

Implement:

```text
zwidyd server
zwidyd client
```

with reconnect support.

## Milestone 3 — Secure Transport

Implement:

```text
QUIC + TLS
```

and authenticated clients.

## Milestone 4 — IPAM

Server allocates stable client IP addresses.

## Milestone 5 — Multiple Clients

Support multiple simultaneous clients.

## Milestone 6 — Isolation

Implement:

```yaml
client_to_client: false
```

and:

```yaml
client_to_client: true
```

## Milestone 7 — Observability

Add:

```text
structured logs
Prometheus metrics
health endpoint
```

## Milestone 8 — Production Deployment

Create:

```text
systemd units
Ansible role
GitHub Actions pipeline
```

## Milestone 9 — Jellyfin Origin

Deploy:

```text
NGINX
   |
Zwidy
   |
Jellyfin
```

and stream a video entirely through the Zwidy network.

---

# 43. Recommended v1 Defaults

```yaml
transport: quic

network:
  cidr: 10.77.0.0/24
  interface: zwidy0
  mtu: 1380
  client_to_client: false

keepalive:
  interval: 15s
  timeout: 45s

logging:
  level: info
  format: json
  output: stdout
```

---

# 44. Core Architectural Principle

Zwidy SHALL separate four concerns:

```text
Transport
    |
    | QUIC / TCP
    v

Tunnel Protocol
    |
    | authentication / framing / session
    v

Virtual Network
    |
    | TUN / IP / routing / IPAM
    v

Application
    |
    | Jellyfin / HTTP / arbitrary TCP/IP
```

Zwidy itself SHALL NOT know or care whether traffic transported through the virtual network is:

```text
HTTP
HTTPS
SSH
Jellyfin
database traffic
or another IP protocol
```

Its responsibility is to provide secure IP connectivity.

---

# 45. Definition of Done for Zwidy v1

Zwidy v1 is considered complete when the following scenario works reliably:

```text
Private home network

Jellyfin
127.0.0.1:8096
    |
zwidyd client
10.77.0.10
    |
    | outbound QUIC connection
    |
Internet
    |
zwidyd server
10.77.0.1
    |
NGINX
    |
ATS
    |
Viewer
```

The Jellyfin machine SHALL require:

```text
no public IP
no inbound firewall rule
no router port forwarding
```

and the viewer SHALL successfully stream video through the CDN infrastructure.

