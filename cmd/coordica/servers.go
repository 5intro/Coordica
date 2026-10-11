package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"strings"
	"time"

	"coordica/internal/apiclient"
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
			server := consoleHTTPServer((&nodeconsole.Server{API: api, UI: web.FS()}).Handler())
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
