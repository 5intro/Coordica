package main

import (
	"context"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"encoding/hex"
	"encoding/pem"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"

	"coordica/internal/apiclient"
	"coordica/internal/domain"
	"coordica/internal/localhttp"
	"coordica/internal/nodeconsole"
	"coordica/internal/runner"
	"coordica/web"
)

type centerEndpoint struct {
	server    *http.Server
	listener  net.Listener
	cert, key string
}

func consoleHTTPServer(handler http.Handler) *http.Server {
	return &http.Server{Handler: handler, ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 90 * time.Second, IdleTimeout: 60 * time.Second, MaxHeaderBytes: 32 << 10}
}

func centerAddresses(address, console, cert, key string) (runnerAddress, consoleAddress string, shared bool, err error) {
	consoleAddress, err = localhttp.Address(console)
	if err != nil {
		return "", "", false, err
	}
	host, port, err := net.SplitHostPort(address)
	if err != nil || port == "" {
		return "", "", false, errors.New("runner listen address must include a host and port")
	}
	if host == "localhost" {
		host = "127.0.0.1"
	}
	runnerAddress = net.JoinHostPort(host, port)
	ip := net.ParseIP(host)
	if (cert == "") != (key == "") {
		return "", "", false, errors.New("both TLS certificate and key are required")
	}
	if cert == "" && (ip == nil || !ip.IsLoopback()) {
		return "", "", false, errors.New("non-loopback listen requires --tls-cert and --tls-key")
	}
	shared = runnerAddress == consoleAddress
	if shared && cert != "" {
		return "", "", false, errors.New("TLS Runner and HTTP console require different listen addresses")
	}
	return runnerAddress, consoleAddress, shared, nil
}

// centerConnectionInfo describes the already bound endpoints. It never trusts
// request authority or forwarding headers and exports certificate blocks only.
func centerConnectionInfo(runnerAddress, consoleAddress, advertised, dataDir string, pair tls.Certificate) (domain.ConnectionInfo, error) {
	info := domain.ConnectionInfo{
		APIVersion: domain.APIVersion, Version: version, ConsoleURL: "http://" + consoleAddress,
		RunnerConsoleURL: "http://127.0.0.1:8081", RunnerListen: runnerAddress, ConsoleListen: consoleAddress, DataDir: dataDir,
	}
	host, _, err := net.SplitHostPort(runnerAddress)
	if err != nil {
		return info, err
	}
	scheme := "http"
	var leaf *x509.Certificate
	if len(pair.Certificate) > 0 {
		scheme = "https"
		leaf, err = x509.ParseCertificate(pair.Certificate[0])
		if err != nil {
			return info, err
		}
		fingerprint := sha256.Sum256(leaf.Raw)
		info.CertificateSHA256 = hex.EncodeToString(fingerprint[:])
		for _, der := range pair.Certificate {
			info.CertificatePEM += string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}))
		}
	}
	if ip := net.ParseIP(host); host != "" && (ip == nil || !ip.IsUnspecified()) && (leaf == nil || leaf.VerifyHostname(host) == nil) {
		info.RunnerURL = scheme + "://" + runnerAddress
	}
	if advertised != "" {
		u, parseErr := url.Parse(advertised)
		if parseErr != nil || u.Scheme != "https" || u.Hostname() == "" || u.User != nil || u.Path != "" && u.Path != "/" || u.RawPath != "" || u.RawQuery != "" || u.Fragment != "" || u.Opaque != "" || u.ForceQuery {
			return info, errors.New("--runner-url must be a plain HTTPS origin without credentials, path, query or fragment")
		}
		if strings.HasSuffix(u.Host, ":") || u.Port() != "" && !validURLPort(u.Port()) {
			return info, errors.New("--runner-url port must be between 1 and 65535")
		}
		if ip := net.ParseIP(u.Hostname()); ip != nil && (ip.IsUnspecified() || ip.IsLoopback()) {
			return info, errors.New("--runner-url must use an address reachable by other computers")
		}
		if strings.EqualFold(u.Hostname(), "localhost") || leaf == nil {
			return info, errors.New("--runner-url requires a remote HTTPS address and a TLS certificate")
		}
		if err := leaf.VerifyHostname(u.Hostname()); err != nil {
			return info, fmt.Errorf("Runner URL is not covered by the TLS certificate: %w", err)
		}
		info.RunnerURL = strings.TrimSuffix(advertised, "/")
	}
	return info, nil
}

func localRunnerConsoleURL(value string) (string, error) {
	if value == "off" {
		return "", nil
	}
	u, err := url.Parse(value)
	if err != nil || u.Scheme != "http" || u.User != nil || u.Path != "" && u.Path != "/" || u.RawPath != "" || u.RawQuery != "" || u.Fragment != "" || u.Opaque != "" || u.ForceQuery {
		return "", errors.New("local Runner console link must be a plain loopback HTTP origin")
	}
	address, err := localhttp.Address(u.Host)
	if err != nil {
		return "", err
	}
	if !validURLPort(u.Port()) {
		return "", errors.New("local Runner console link port must be between 1 and 65535")
	}
	return "http://" + address, nil
}

func validURLPort(value string) bool {
	port, err := strconv.Atoi(value)
	return err == nil && port > 0 && port <= 65535
}

func runRunner(ctx context.Context, args []string) error {
	f := flag.NewFlagSet("runner", flag.ContinueOnError)
	dir := f.String("data", ".coordica-runner", "Runner directory")
	console := f.String("console-listen", "", "Machine-local HTTP console address (default 127.0.0.1:8081; off disables)")
	if err := f.Parse(args); err != nil {
		return err
	}
	cfg, err := runner.Load(*dir)
	if err != nil {
		return err
	}
	if err = runner.ValidateHTTPSProxy(cfg.HTTPSProxy); err != nil {
		return err
	}
	if cfg.HTTPSProxy != "" {
		if err = os.Setenv("HTTPS_PROXY", cfg.HTTPSProxy); err != nil {
			return err
		}
	}
	// Restore only the initialization-time PATH, not arbitrary model credentials.
	if cfg.RuntimePath != "" {
		if err = os.Setenv("PATH", cfg.RuntimePath); err != nil {
			return err
		}
	}
	if cfg.NodeID == "" || cfg.Token == "" {
		return errors.New("Runner must be paired first")
	}
	address := strings.TrimSpace(*console)
	if address == "" {
		address = cfg.ConsoleListen
	}
	if address == "" {
		address = "127.0.0.1:8081"
	}
	if address != "off" {
		address, err = localhttp.Address(address)
		if err != nil {
			return err
		}
	}
	r, err := runner.New(cfg)
	if err != nil {
		return err
	}
	if address != "off" {
		api, err := apiclient.New(cfg.CenterURL, cfg.Token, cfg.TLSCA)
		if err != nil {
			return err
		}
		listener, err := net.Listen("tcp", address)
		if err != nil {
			// Visibility is optional. A busy viewer port must not stop execution.
			slog.Warn("Runner console unavailable; execution continues", "error", err, "console", address)
		} else {
			defer listener.Close()
			server := consoleHTTPServer((&nodeconsole.Server{API: api, UI: web.FS(), CenterURL: cfg.CenterURL}).Handler())
			go func() {
				if err := server.Serve(listener); err != nil && !errors.Is(err, http.ErrServerClosed) {
					slog.Warn("Runner console stopped; execution continues", "error", err)
				}
			}()
			defer func() {
				shutdown, cancel := context.WithTimeout(context.Background(), 5*time.Second)
				defer cancel()
				if err := server.Shutdown(shutdown); err != nil {
					_ = server.Close()
				}
			}()
			slog.Info("Runner console ready", "console", fmt.Sprint("http://", listener.Addr()))
		}
	}
	return r.Run(ctx)
}
