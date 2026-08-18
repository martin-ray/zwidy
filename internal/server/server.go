package server

import (
	"bufio"
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"sync"
	"time"

	"zwidy/internal/config"
	"zwidy/internal/ipam"
	"zwidy/internal/logging"
	"zwidy/internal/metrics"
	"zwidy/internal/protocol"
	"zwidy/internal/routing"
	"zwidy/internal/transport"
	"zwidy/internal/tun"
)

type session struct {
	nodeID    string
	virtualIP string
	conn      net.Conn
	reader    *bufio.Reader
	lastSeen  time.Time
	mu        sync.Mutex
}

type registry struct {
	mu      sync.RWMutex
	byIP    map[string]*session
	byNode  map[string]*session
	clients map[string]config.ClientConfig
}

func Run(ctx context.Context, cfg *config.Config) error {
	logger, closer, err := logging.New(cfg.Logging)
	if err != nil {
		return err
	}
	defer closer.Close()

	metricsReg := metrics.New("server")
	metricsServer := startMetricsServer(ctx, cfg, metricsReg, logger)
	defer shutdownHTTP(metricsServer)

	staticClients := map[string]string{}
	authClients := map[string]config.ClientConfig{}
	for _, client := range cfg.Clients {
		if client.IP != "" {
			staticClients[client.NodeID] = client.IP
		}
		authClients[client.NodeID] = client
	}
	ipamStore, err := ipam.Open(cfg.IPAM.Database, cfg.Network.CIDR, cfg.Network.ServerIP, staticClients)
	if err != nil {
		return err
	}
	dev, err := tun.Open(cfg.Network.Interface, fmt.Sprintf("%s/%s", cfg.Network.ServerIP, maskBits(cfg.Network.CIDR)), cfg.Network.MTU)
	if err != nil {
		return err
	}
	defer dev.Close()
	if err := routing.AddRoute(cfg.Network.CIDR, dev.Name); err != nil {
		logger.Warn("route add failed", map[string]any{"component": "routing", "error": err.Error()})
		defer routing.DeleteRoute(cfg.Network.CIDR, dev.Name)
	}
	tlsCfg, err := transport.ServerTLSConfig(cfg)
	if err != nil {
		return err
	}
	if cfg.Listen.Transport == "quic" {
		return errors.New("quic transport is configured but not implemented yet; use transport: tcp")
	}
	ln, err := tls.Listen("tcp", net.JoinHostPort(cfg.Listen.Address, fmt.Sprintf("%d", cfg.Listen.Port)), tlsCfg)
	if err != nil {
		return err
	}
	defer ln.Close()
	logger.Info("daemon startup", map[string]any{"component": "server", "listen": ln.Addr().String(), "transport": cfg.Listen.Transport})

	reg := &registry{byIP: map[string]*session{}, byNode: map[string]*session{}, clients: authClients}
	errCh := make(chan error, 2)
	go acceptLoop(ctx, ln, reg, ipamStore, dev, cfg, logger, metricsReg, errCh)
	go tunLoop(ctx, reg, dev, cfg, logger, metricsReg, errCh)

	select {
	case <-ctx.Done():
		logger.Info("daemon shutdown", map[string]any{"component": "server"})
		return nil
	case err := <-errCh:
		return err
	}
	}

func acceptLoop(ctx context.Context, ln net.Listener, reg *registry, ipamStore *ipam.Store, dev *tun.Device, cfg *config.Config, logger *logging.Logger, m *metrics.Registry, errCh chan<- error) {
	for {
		conn, err := ln.Accept()
		if err != nil {
			if ctx.Err() != nil {
				return
			}
			m.ConnectionErrorsTotal.Add(1)
			errCh <- err
			return
		}
		m.ConnectionsTotal.Add(1)
		go handleClient(ctx, conn, reg, ipamStore, dev, cfg, logger, m)
	}
	}

func handleClient(ctx context.Context, conn net.Conn, reg *registry, ipamStore *ipam.Store, dev *tun.Device, cfg *config.Config, logger *logging.Logger, m *metrics.Registry) {
	defer conn.Close()
	reader := bufio.NewReader(conn)
	_ = conn.SetDeadline(time.Now().Add(cfg.Keepalive.Timeout))
	hello, err := protocol.Receive(reader, cfg.Network.MaxPacketSize)
	if err != nil || hello.Type != "HELLO" || hello.ProtocolVersion != protocol.Version {
		_ = protocol.Send(conn, protocol.Message{Type: "ERROR", Error: "invalid hello"})
		m.AuthFailuresTotal.Add(1)
		return
	}
	auth, err := protocol.Receive(reader, cfg.Network.MaxPacketSize)
	if err != nil || auth.Type != "AUTH" {
		_ = protocol.Send(conn, protocol.Message{Type: "AUTH_ERROR", Error: "missing auth"})
		m.AuthFailuresTotal.Add(1)
		return
	}
	if !reg.authorized(hello.NodeID, auth.Token) {
		_ = protocol.Send(conn, protocol.Message{Type: "AUTH_ERROR", Error: "authentication failed"})
		m.AuthFailuresTotal.Add(1)
		logger.Warn("authentication failure", map[string]any{"component": "auth", "node_id": hello.NodeID, "remote_address": conn.RemoteAddr().String()})
		return
	}
	virtualIP, err := ipamStore.Allocate(hello.NodeID)
	if err != nil {
		_ = protocol.Send(conn, protocol.Message{Type: "ERROR", Error: err.Error()})
		return
	}
	sess := &session{nodeID: hello.NodeID, virtualIP: virtualIP, conn: conn, reader: reader, lastSeen: time.Now()}
	reg.add(sess)
	defer reg.remove(sess)
	m.ConnectedClients.Add(1)
	defer m.ConnectedClients.Add(-1)
	_ = protocol.Send(conn, protocol.Message{Type: "AUTH_OK"})
	_ = protocol.Send(conn, protocol.Message{Type: "IP_ASSIGN", VirtualIP: virtualIP, NetworkCIDR: cfg.Network.CIDR})
	logger.Info("client connected", map[string]any{"component": "tunnel", "node_id": hello.NodeID, "virtual_ip": virtualIP, "remote_address": conn.RemoteAddr().String()})

	for {
		_ = conn.SetDeadline(time.Now().Add(cfg.Keepalive.Timeout))
		msg, err := protocol.Receive(reader, cfg.Network.MaxPacketSize)
		if err != nil {
			logger.Info("client disconnected", map[string]any{"component": "tunnel", "node_id": hello.NodeID, "error": err.Error()})
			return
		}
		sess.lastSeen = time.Now()
		switch msg.Type {
		case "DATA":
			packet := msg.Payload
			m.PacketsReceivedTotal.Add(1)
			m.BytesReceivedTotal.Add(int64(len(packet)))
			if len(packet) < 20 || sourceIPv4(packet) != virtualIP {
				m.PacketsDroppedTotal.Add(1)
				logger.Warn("spoofed packet dropped", map[string]any{"component": "security", "node_id": hello.NodeID, "virtual_ip": virtualIP})
				continue
			}
			if _, err := dev.Write(packet); err != nil {
				logger.Error("tun write failed", map[string]any{"component": "tun", "error": err.Error()})
				return
			}
		case "PING":
			_ = protocol.Send(conn, protocol.Message{Type: "PONG", TimestampUnix: time.Now().Unix()})
		case "DISCONNECT":
			return
		}
		if ctx.Err() != nil {
			return
		}
	}
	}

func tunLoop(ctx context.Context, reg *registry, dev *tun.Device, cfg *config.Config, logger *logging.Logger, m *metrics.Registry, errCh chan<- error) {
	buf := make([]byte, cfg.Network.MaxPacketSize)
	for {
		n, err := dev.Read(buf)
		if err != nil {
			if ctx.Err() != nil {
				return
			}
			errCh <- err
			return
		}
		packet := append([]byte(nil), buf[:n]...)
		destIP := destIPv4(packet)
		srcIP := sourceIPv4(packet)
		target := reg.byVirtualIP(destIP)
		if target == nil {
			continue
		}
		if !cfg.Network.ClientToClient && reg.isClientIP(srcIP) && reg.isClientIP(destIP) {
			m.PacketsDroppedTotal.Add(1)
			logger.Debug("client isolation dropped packet", map[string]any{"component": "routing", "src": srcIP, "dst": destIP})
			continue
		}
		if err := target.send(protocol.Message{Type: "DATA", Payload: packet}); err != nil {
			m.ConnectionErrorsTotal.Add(1)
			logger.Warn("forward failed", map[string]any{"component": "tunnel", "node_id": target.nodeID, "error": err.Error()})
			continue
		}
		m.PacketsSentTotal.Add(1)
		m.BytesSentTotal.Add(int64(len(packet)))
	}
	}

func (r *registry) authorized(nodeID, token string) bool {
	if nodeID == "" {
		return false
	}
	client, ok := r.clients[nodeID]
	if !ok {
		return false
	}
	if client.Token == "" {
		return token != ""
	}
	return client.Token == token
	}

func (r *registry) add(sess *session) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.byIP[sess.virtualIP] = sess
	r.byNode[sess.nodeID] = sess
	}

func (r *registry) remove(sess *session) {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.byIP, sess.virtualIP)
	delete(r.byNode, sess.nodeID)
	}

func (r *registry) byVirtualIP(ip string) *session {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.byIP[ip]
	}

func (r *registry) isClientIP(ip string) bool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	_, ok := r.byIP[ip]
	return ok
	}

func (s *session) send(msg protocol.Message) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return protocol.Send(s.conn, msg)
	}

func startMetricsServer(ctx context.Context, cfg *config.Config, reg *metrics.Registry, logger *logging.Logger) *http.Server {
	if !cfg.Metrics.Enabled {
		return nil
	}
	srv := &http.Server{Addr: cfg.Metrics.Address, Handler: reg.Handler()}
	go func() {
		logger.Info("metrics server started", map[string]any{"component": "metrics", "address": cfg.Metrics.Address})
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			logger.Warn("metrics server stopped", map[string]any{"component": "metrics", "error": err.Error()})
		}
	}()
	go func() {
		<-ctx.Done()
		shutdownHTTP(srv)
	}()
	return srv
	}

func shutdownHTTP(srv *http.Server) {
	if srv == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	_ = srv.Shutdown(ctx)
	}

func maskBits(cidr string) string {
	_, network, _ := net.ParseCIDR(cidr)
	ones, _ := network.Mask.Size()
	return fmt.Sprintf("%d", ones)
	}

func sourceIPv4(packet []byte) string {
	if len(packet) < 20 {
		return ""
	}
	return net.IP(packet[12:16]).String()
	}

func destIPv4(packet []byte) string {
	if len(packet) < 20 {
		return ""
	}
	return net.IP(packet[16:20]).String()
	}

var _ = os.ErrClosed
