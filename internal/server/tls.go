// Package server implements the raxd TLS HTTP transport.
package server

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"errors"
	"math/big"
	"net"
	"os"
	"path/filepath"
	"time"
)

// ErrTLSCert is returned when the existing cert/key files exist but cannot be
// loaded (empty, corrupt, or mismatched pair). Per SR-6 the files are NOT
// overwritten; the caller must remove them manually.
var ErrTLSCert = errors.New("TLS certificate or key is corrupted or unreadable")

const (
	certFile = "cert.pem"
	keyFile  = "key.pem"
)

// certResult carries the loaded/generated certificate and whether it was newly created.
type certResult struct {
	cert      tls.Certificate
	generated bool
}

// loadOrCreateCert loads the TLS key-pair from tlsDir if both files exist, or
// generates a new self-signed ECDSA P-256 certificate and writes it to tlsDir.
//
// Contracts (plan.md, AC2/AC3, SR-3/SR-4/SR-5/SR-6):
//   - Existing valid pair → loaded via tls.LoadX509KeyPair (no regeneration).
//   - Missing pair → generate ECDSA P-256, SAN 127.0.0.1+localhost, key 0600, cert 0644.
//   - Empty/corrupt existing files → return ErrTLSCert (no overwrite, no panic).
func loadOrCreateCert(tlsDir string) (certResult, error) {
	certPath := filepath.Join(tlsDir, certFile)
	keyPath := filepath.Join(tlsDir, keyFile)

	// Check whether both files exist.
	certExists := fileExists(certPath)
	keyExists := fileExists(keyPath)

	switch {
	case certExists && keyExists:
		// Both files present — try to load. SR-5: reuse existing pair.
		cert, err := tls.LoadX509KeyPair(certPath, keyPath)
		if err != nil {
			// Files are corrupt/mismatched. SR-6: do NOT overwrite, return sentinel.
			return certResult{}, ErrTLSCert
		}
		return certResult{cert: cert, generated: false}, nil

	case certExists || keyExists:
		// Only one file present — partial state, treat as corrupt (SR-6).
		return certResult{}, ErrTLSCert

	default:
		// Neither file exists — generate a new pair (AC2, SR-3).
		cert, err := generateSelfSigned(certPath, keyPath)
		if err != nil {
			return certResult{}, err
		}
		return certResult{cert: cert, generated: true}, nil
	}
}

// generateSelfSigned creates a new ECDSA P-256 self-signed certificate with
// SAN for 127.0.0.1 and localhost, writes cert.pem (0644) and key.pem (0600)
// to the given paths, and returns the loaded tls.Certificate.
func generateSelfSigned(certPath, keyPath string) (tls.Certificate, error) {
	// Generate ECDSA P-256 private key (SR-3, research.md §TLS).
	priv, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return tls.Certificate{}, errors.New("failed to generate TLS certificate")
	}

	// Build certificate template (SR-3: SAN, NotBefore/NotAfter, KeyUsage).
	serial, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
	if err != nil {
		return tls.Certificate{}, errors.New("failed to generate TLS certificate")
	}

	now := time.Now().UTC()
	tmpl := &x509.Certificate{
		SerialNumber: serial,
		Subject: pkix.Name{
			CommonName:   "raxd",
			Organization: []string{"raxd"},
		},
		NotBefore:             now.Add(-time.Minute),   // small skew tolerance
		NotAfter:              now.Add(10 * 365 * 24 * time.Hour), // 10 years
		KeyUsage:              x509.KeyUsageDigitalSignature | x509.KeyUsageCertSign,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true,
		IsCA:                  true,
		IPAddresses:           []net.IP{net.ParseIP("127.0.0.1")},
		DNSNames:              []string{"localhost"},
	}

	// self-signed: template == parent (research.md §self-signed).
	certDER, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &priv.PublicKey, priv)
	if err != nil {
		return tls.Certificate{}, errors.New("failed to generate TLS certificate")
	}

	// Marshal private key as PKCS#8 PEM (research.md §ECDSA).
	privDER, err := x509.MarshalPKCS8PrivateKey(priv)
	if err != nil {
		return tls.Certificate{}, errors.New("failed to generate TLS certificate")
	}

	// Write key.pem with permissions 0600 BEFORE writing cert.pem (SR-4).
	// Use atomic write: write to temp, chmod, rename.
	if err := writePEM(keyPath, "PRIVATE KEY", privDER, 0o600); err != nil {
		return tls.Certificate{}, errors.New("failed to generate TLS certificate")
	}

	// Write cert.pem with permissions 0644 (SR-4).
	if err := writePEM(certPath, "CERTIFICATE", certDER, 0o644); err != nil {
		// Best-effort cleanup of key file on cert write failure.
		_ = os.Remove(keyPath)
		return tls.Certificate{}, errors.New("failed to generate TLS certificate")
	}

	// Load the written pair to return a valid tls.Certificate.
	cert, err := tls.LoadX509KeyPair(certPath, keyPath)
	if err != nil {
		return tls.Certificate{}, errors.New("failed to generate TLS certificate")
	}
	return cert, nil
}

// writePEM writes data as a PEM block with the given type to path, setting
// file permissions to perm. Uses atomic write (temp→chmod→rename) so the
// final file never appears with wrong permissions.
func writePEM(path, blockType string, data []byte, perm os.FileMode) error {
	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, ".raxd-tls-*.tmp")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()

	// Set permissions BEFORE writing content (SR-4).
	if err := tmp.Chmod(perm); err != nil {
		_ = tmp.Close()
		_ = os.Remove(tmpName)
		return err
	}

	if err := pem.Encode(tmp, &pem.Block{Type: blockType, Bytes: data}); err != nil {
		_ = tmp.Close()
		_ = os.Remove(tmpName)
		return err
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		_ = os.Remove(tmpName)
		return err
	}
	if err := tmp.Close(); err != nil {
		_ = os.Remove(tmpName)
		return err
	}
	return os.Rename(tmpName, path)
}

// fileExists returns true if a file exists at path and has a non-zero size.
// An empty file is treated as non-existent (SR-6: empty cert → ErrTLSCert).
func fileExists(path string) bool {
	info, err := os.Stat(path)
	if err != nil {
		return false
	}
	return info.Size() > 0
}
