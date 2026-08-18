package transport

import (
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"os"

	"zwidy/internal/config"
)

func ServerTLSConfig(cfg *config.Config) (*tls.Config, error) {
	cert, err := tls.LoadX509KeyPair(cfg.TLS.Certificate, cfg.TLS.PrivateKey)
	if err != nil {
		return nil, err
	}
	tlsCfg := &tls.Config{Certificates: []tls.Certificate{cert}, MinVersion: tls.VersionTLS13}
	if cfg.TLS.CA != "" {
		pool := x509.NewCertPool()
		b, err := os.ReadFile(cfg.TLS.CA)
		if err != nil {
			return nil, err
		}
		if !pool.AppendCertsFromPEM(b) {
			return nil, fmt.Errorf("failed to parse tls.ca")
		}
		tlsCfg.ClientCAs = pool
		tlsCfg.ClientAuth = tls.VerifyClientCertIfGiven
	}
	return tlsCfg, nil
	}

func ClientTLSConfig(cfg *config.Config) (*tls.Config, error) {
	tlsCfg := &tls.Config{MinVersion: tls.VersionTLS13, ServerName: cfg.TLS.ServerName}
	if cfg.TLS.CA != "" {
		pool := x509.NewCertPool()
		b, err := os.ReadFile(cfg.TLS.CA)
		if err != nil {
			return nil, err
		}
		if !pool.AppendCertsFromPEM(b) {
			return nil, fmt.Errorf("failed to parse tls.ca")
		}
		tlsCfg.RootCAs = pool
	}
	if cfg.Auth.CredentialFile != "" && cfg.Auth.PrivateKeyFile != "" {
		cert, err := tls.LoadX509KeyPair(cfg.Auth.CredentialFile, cfg.Auth.PrivateKeyFile)
		if err != nil {
			return nil, err
		}
		tlsCfg.Certificates = []tls.Certificate{cert}
	}
	return tlsCfg, nil
	}
