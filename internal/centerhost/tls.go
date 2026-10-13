package centerhost

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
	"net/url"
	"os"
	"path/filepath"
	"time"

	"coordica/internal/localfile"
)

func ensureTLS(c Config) error {
	certPath, keyPath := filepath.Join(c.DataDir, "center.crt"), filepath.Join(c.DataDir, "center.key")
	u, _ := url.Parse(c.RunnerURL)
	if pair, e := tls.LoadX509KeyPair(certPath, keyPath); e == nil {
		leaf, e := x509.ParseCertificate(pair.Certificate[0])
		if e != nil {
			return e
		}
		if time.Now().After(leaf.NotAfter) {
			return errors.New("saved center TLS certificate expired; explicit Owner renewal required")
		}
		return leaf.VerifyHostname(u.Hostname())
	}
	if _, e := os.Stat(certPath); e == nil {
		return errors.New("saved center TLS pair invalid; preserved")
	} else if !os.IsNotExist(e) {
		return e
	}
	var key *ecdsa.PrivateKey
	if raw, e := os.ReadFile(keyPath); e == nil {
		block, _ := pem.Decode(raw)
		if block == nil {
			return errors.New("saved TLS key invalid; preserved")
		}
		parsed, e := x509.ParsePKCS8PrivateKey(block.Bytes)
		if e != nil {
			return e
		}
		var ok bool
		key, ok = parsed.(*ecdsa.PrivateKey)
		if !ok {
			return errors.New("saved TLS key type conflicts")
		}
	} else if os.IsNotExist(e) {
		var e error
		key, e = ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
		if e != nil {
			return e
		}
		raw, e := x509.MarshalPKCS8PrivateKey(key)
		if e != nil {
			return e
		}
		if e = localfile.WritePrivate(keyPath, pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: raw})); e != nil {
			return e
		}
	} else {
		return e
	}
	serial, e := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
	if e != nil {
		return e
	}
	now := time.Now()
	template := &x509.Certificate{SerialNumber: serial, Subject: pkix.Name{CommonName: "Coordica fixed center"}, NotBefore: now.Add(-time.Minute), NotAfter: now.AddDate(1, 0, 0), IsCA: true, BasicConstraintsValid: true, KeyUsage: x509.KeyUsageDigitalSignature | x509.KeyUsageCertSign, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}}
	host, _, _ := net.SplitHostPort(c.RunnerListen)
	for _, name := range []string{u.Hostname(), host, "localhost", "127.0.0.1", "::1"} {
		if ip := net.ParseIP(name); ip != nil {
			template.IPAddresses = append(template.IPAddresses, ip)
		} else {
			template.DNSNames = append(template.DNSNames, name)
		}
	}
	der, e := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	if e != nil {
		return e
	}
	return localfile.WritePrivate(certPath, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}))
}
