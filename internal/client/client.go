package client

import (
	"bufio"
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"math/rand"
	"net"
	"net/http"
	"time"

	"zwidy/internal/config"
	"zwidy/internal/logging"
	"zwidy/internal/metrics"
	"zwidy/internal/protocol"
	"zwidy/internal/routing"
	"zwidy/internal/transport"
	"zwidy/internal/tun"
)

func Run(ctx context.Context, cfg *config.Config) error {
	logger, closer, err := logging.New(cfg.Logging)
	if err != nil {
		return err
	}
	defer closer.Close()
	metricsReg := metrics.New("client")
	metricsServer := startMetricsServer(ctx, cfg, metricsReg, logger)
	defer shutdownHTTP(metricsServer)
	if cfg.Server.Transport == "quic" {
		return errors.New("quic transport is configured but not implemented yet; use transport: tcp")
	}
	tlsCfg, err := transport.ClientTLSConfig(cfg)
	if err != nil {
		return err
	}
	var attempt int
	for {
		err := runSession(ctx, cfg, tlsCfg, logger, metricsReg)
		if err == nil || ctx.Err() != nil {
			logger.Info("daemon shutdown", map[string]any{"component": "client"})
			return err
		}
		metricsReg.ConnectionErrorsTotal.Add(1)
		metricsReg.ClientReconnectsTotal.Add(1)
		attempt++
		delay := backoff(cfg.Reconnect.MinDelay, cfg.Reconnect.MaxDelay, attempt)
		logger.Warn("tunnel reconnect", map[string]any{"component": "client", "delay": delay.String(), "error": err.Error()})
		select {
		case <-ctx.Done():
			return nil
		case <-time.After(delay):
		}
	}
	}

func runSession(ctx context.Context, cfg *config.Config, tlsCfg *tls.Config, logger *logging.Logger, m *metrics.Registry) error {
	addr := net.JoinHostPort(cfg.Server.Address, fmt.Sprintf("%d", cfg.Server.Port))
	conn, err := tls.Dial("tcp", addr, tlsCfg)
	if err != nil {
		return err
	}
	defer conn.Close()
	m.ConnectionsTotal.Add(1)
	if err := protocol.Send(conn, protocol.Message{Type: "HELLO", ProtocolVersion: protocol.Version, NodeID: cfg.Node.ID}); err != nil {
		return err
	}
	if err := protocol.Send(conn, protocol.Message{Type: "AUTH", NodeID: cfg.Node.ID, Token: cfg.Auth.Token}); err != nil {
		return err
	}
	reader := bufio.NewReader(conn)
	authOK, err := protocol.Receive(reader, cfg.Network.MaxPacketSize)
	if err != nil {
		return err
	}
	if authOK.Type != "AUTH_OK" {
		return fmt.Errorf("authentication failed: %s", authOK.Error)
	}
	assign, err := protocol.Receive(reader, cfg.Network.MaxPacketSize)
	if err != nil {
		return err
	}
	if assign.Type != "IP_ASSIGN" || assign.VirtualIP == "" {
		return fmt.Errorf("missing IP assignment")
	}
	dev, err := tun.Open(cfg.Network.Interface, fmt.Sprintf("%s/32", assign.VirtualIP), cfg.Network.MTU)
	if err != nil {
		return err
	}
	defer dev.Close()
	routeCIDR := assign.NetworkCIDR
	if routeCIDR == "" {
		routeCIDR = cfg.Network.CIDR
	}
	if routeCIDR == "" {
		return fmt.Errorf("missing network CIDR in IP assignment and client config")
	}
	if err := routing.AddRoute(routeCIDR, dev.Name); err != nil {
		logger.Debug("default route not installed", map[string]any{"component": "routing", "error": err.Error()})
	} else {
		defer routing.DeleteRoute(routeCIDR, dev.Name)
	}
	logger.Info("client connected", map[string]any{"component": "tunnel", "node_id": cfg.Node.ID, "virtual_ip": assign.VirtualIP, "remote_address": addr})
	errCh := make(chan error, 2)
	go receiveLoop(ctx, conn, reader, dev, cfg, logger, m, errCh)
	go sendLoop(ctx, conn, dev, cfg, logger, m, errCh)
	go keepaliveLoop(ctx, conn, cfg, errCh)
	select {
	case <-ctx.Done():
		_ = protocol.Send(conn, protocol.Message{Type: "DISCONNECT"})
		return nil
	case err := <-errCh:
		return err
	}
	}

func receiveLoop(ctx context.Context, conn net.Conn, reader *bufio.Reader, dev *tun.Device, cfg *config.Config, logger *logging.Logger, m *metrics.Registry, errCh chan<- error) {
	for {
		_ = conn.SetDeadline(time.Now().Add(cfg.Keepalive.Timeout))
		msg, err := protocol.Receive(reader, cfg.Network.MaxPacketSize)
		if err != nil {
			errCh <- err
			return
		}
		switch msg.Type {
		case "DATA":
			if _, err := dev.Write(msg.Payload); err != nil {
				errCh <- err
				return
			}
			m.PacketsReceivedTotal.Add(1)
			m.BytesReceivedTotal.Add(int64(len(msg.Payload)))
		case "PING":
			_ = protocol.Send(conn, protocol.Message{Type: "PONG", TimestampUnix: time.Now().Unix()})
		case "ERROR", "AUTH_ERROR":
			errCh <- fmt.Errorf(msg.Error)
			return
		}
		if ctx.Err() != nil {
			return
		}
	}
	}

func sendLoop(ctx context.Context, conn net.Conn, dev *tun.Device, cfg *config.Config, logger *logging.Logger, m *metrics.Registry, errCh chan<- error) {
	buf := make([]byte, cfg.Network.MaxPacketSize)
	for {
		n, err := dev.Read(buf)
		if err != nil {
			errCh <- err
			return
		}
		packet := append([]byte(nil), buf[:n]...)
		if err := protocol.Send(conn, protocol.Message{Type: "DATA", Payload: packet}); err != nil {
			errCh <- err
			return
		}
		m.PacketsSentTotal.Add(1)
		m.BytesSentTotal.Add(int64(len(packet)))
		if ctx.Err() != nil {
			return
		}
	}
	}

func keepaliveLoop(ctx context.Context, conn net.Conn, cfg *config.Config, errCh chan<- error) {
	ticker := time.NewTicker(cfg.Keepalive.Interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if err := protocol.Send(conn, protocol.Message{Type: "PING", TimestampUnix: time.Now().Unix()}); err != nil {
				errCh <- err
				return
			}
		}
	}
	}

func backoff(min, max time.Duration, attempt int) time.Duration {
	delay := min << (attempt - 1)
	if delay > max {
		delay = max
	}
	jitter := time.Duration(rand.Int63n(int64(min)))
	return delay + jitter/2
	}

func startMetricsServer(ctx context.Context, cfg *config.Config, reg *metrics.Registry, logger *logging.Logger) *http.Server {
	return serverStartMetrics(ctx, cfg, reg, logger)
	}

func shutdownHTTP(srv *http.Server) { serverShutdownHTTP(srv) }

var serverStartMetrics = serverStartMetricsFunc
var serverShutdownHTTP = serverShutdownHTTPFunc
