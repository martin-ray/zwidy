package transport

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"

	"zwidy/internal/config"
)

func TestServerAndClientTLSConfigHandshakeWithClientCert(t *testing.T) {
	tempDir := t.TempDir()
	caCertPEM, caKey, caCert := mustCreateCA(t)
	serverCertPEM, serverKeyPEM := mustCreateLeaf(t, caKey, caCert, leafOptions{
		commonName:  "localhost",
		dnsNames:    []string{"localhost"},
		extKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
	})
	clientCertPEM, clientKeyPEM := mustCreateLeaf(t, caKey, caCert, leafOptions{
		commonName:  "client-node",
		extKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth},
	})

	mustWriteFile(t, filepath.Join(tempDir, "ca.crt"), caCertPEM)
	mustWriteFile(t, filepath.Join(tempDir, "server.crt"), serverCertPEM)
	mustWriteFile(t, filepath.Join(tempDir, "server.key"), serverKeyPEM)
	mustWriteFile(t, filepath.Join(tempDir, "client.crt"), clientCertPEM)
	mustWriteFile(t, filepath.Join(tempDir, "client.key"), clientKeyPEM)

	serverCfg := &config.Config{}
	serverCfg.TLS.Certificate = filepath.Join(tempDir, "server.crt")
	serverCfg.TLS.PrivateKey = filepath.Join(tempDir, "server.key")
	serverCfg.TLS.CA = filepath.Join(tempDir, "ca.crt")
	serverTLS, err := ServerTLSConfig(serverCfg)
	if err != nil {
		t.Fatal(err)
	}

	clientCfg := &config.Config{}
	clientCfg.TLS.CA = filepath.Join(tempDir, "ca.crt")
	clientCfg.TLS.ServerName = "localhost"
	clientCfg.Auth.CredentialFile = filepath.Join(tempDir, "client.crt")
	clientCfg.Auth.PrivateKeyFile = filepath.Join(tempDir, "client.key")
	clientTLS, err := ClientTLSConfig(clientCfg)
	if err != nil {
		t.Fatal(err)
	}

	ln, err := tls.Listen("tcp", "127.0.0.1:0", serverTLS)
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()

	errCh := make(chan error, 1)
	go func() {
		conn, err := ln.Accept()
		if err != nil {
			errCh <- err
			return
		}
		defer conn.Close()
		tlsConn := conn.(*tls.Conn)
		if err := tlsConn.Handshake(); err != nil {
			errCh <- err
			return
		}
		state := tlsConn.ConnectionState()
		if len(state.PeerCertificates) == 0 {
			errCh <- os.ErrPermission
			return
		}
		peer := state.PeerCertificates[0]
		if peer.Subject.CommonName != "client-node" {
			errCh <- os.ErrInvalid
			return
		}
		errCh <- nil
	}()

	conn, err := tls.Dial("tcp", ln.Addr().String(), clientTLS)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	if err := <-errCh; err != nil {
		t.Fatal(err)
	}
}

type leafOptions struct {
	commonName  string
	dnsNames    []string
	ipAddresses []net.IP
	extKeyUsage []x509.ExtKeyUsage
}

func mustCreateCA(t *testing.T) ([]byte, *rsa.PrivateKey, *x509.Certificate) {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	tpl := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject: pkix.Name{CommonName: "Zwidy Test CA"},
		NotBefore: time.Now().Add(-time.Hour),
		NotAfter: time.Now().Add(24 * time.Hour),
		KeyUsage: x509.KeyUsageCertSign | x509.KeyUsageCRLSign,
		BasicConstraintsValid: true,
		IsCA: true,
	}
	der, err := x509.CreateCertificate(rand.Reader, tpl, tpl, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	cert, err := x509.ParseCertificate(der)
	if err != nil {
		t.Fatal(err)
	}
	return pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), key, cert
}

func mustCreateLeaf(t *testing.T, caKey *rsa.PrivateKey, caCert *x509.Certificate, opts leafOptions) ([]byte, []byte) {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	tpl := &x509.Certificate{
		SerialNumber: big.NewInt(time.Now().UnixNano()),
		Subject: pkix.Name{CommonName: opts.commonName},
		NotBefore: time.Now().Add(-time.Hour),
		NotAfter: time.Now().Add(24 * time.Hour),
		KeyUsage: x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
		ExtKeyUsage: opts.extKeyUsage,
		DNSNames: opts.dnsNames,
		IPAddresses: opts.ipAddresses,
		BasicConstraintsValid: true,
	}
	der, err := x509.CreateCertificate(rand.Reader, tpl, caCert, &key.PublicKey, caKey)
	if err != nil {
		t.Fatal(err)
	}
	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(key)})
	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
	return certPEM, keyPEM
}

func mustWriteFile(t *testing.T, path string, data []byte) {
	t.Helper()
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
}
