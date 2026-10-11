package main

import (
	"coordica/internal/localfile"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"errors"
	"flag"
	"fmt"
	"math/big"
	"net"
	"os"
	"path/filepath"
	"strings"
	"time"
)

func certificate(args []string) error {
	f := flag.NewFlagSet("tls", flag.ContinueOnError)
	dir := f.String("out", ".coordica-tls", "New certificate directory")
	hosts := f.String("hosts", "localhost,127.0.0.1", "Comma-separated center DNS names/IPs")
	if e := f.Parse(args); e != nil {
		return e
	}
	if _, e := os.Stat(*dir); !errors.Is(e, os.ErrNotExist) {
		return errors.New("TLS output directory must not exist")
	}
	if e := os.MkdirAll(*dir, 0700); e != nil {
		return e
	}
	key, e := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if e != nil {
		return e
	}
	serial, e := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
	if e != nil {
		return e
	}
	now := time.Now()
	template := &x509.Certificate{SerialNumber: serial, Subject: pkix.Name{CommonName: "Coordica local center"}, NotBefore: now.Add(-time.Minute), NotAfter: now.AddDate(1, 0, 0), IsCA: true, BasicConstraintsValid: true, KeyUsage: x509.KeyUsageDigitalSignature | x509.KeyUsageCertSign, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}}
	for _, host := range strings.Split(*hosts, ",") {
		host = strings.TrimSpace(host)
		if host == "" {
			continue
		}
		if ip := net.ParseIP(host); ip != nil {
			template.IPAddresses = append(template.IPAddresses, ip)
		} else {
			template.DNSNames = append(template.DNSNames, host)
		}
	}
	der, e := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	if e != nil {
		return e
	}
	private, e := x509.MarshalPKCS8PrivateKey(key)
	if e != nil {
		return e
	}
	if e = os.WriteFile(filepath.Join(*dir, "center.crt"), pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), 0644); e != nil {
		return e
	}
	if e = localfile.WritePrivate(filepath.Join(*dir, "center.key"), pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: private})); e != nil {
		return e
	}
	fmt.Println("Certificate created:", filepath.Join(*dir, "center.crt"))
	return nil
}
