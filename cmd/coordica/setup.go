package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"

	"coordica/internal/mcpconfig"
)

// setup installs only a local MCP launcher. Pairing and Runner lifecycle remain
// the responsibility of the existing bootstrap and onboarding use cases.
func setup(ctx context.Context, args []string) error {
	f := flag.NewFlagSet("setup", flag.ContinueOnError)
	client := f.String("client", "auto", "MCP client: auto, claude, codex, cursor, kimi, pi, dsh, generic")
	apply := f.Bool("apply", false, "Apply the setup plan (default: preview only)")
	binary := f.String("binary", "", "Fixed absolute Coordica executable path (default: this executable)")
	data := f.String("runner-data", "", "Optional fixed absolute Runner data directory")
	output := f.String("output", "", "Directory for generated dsh overlay or generic MCP JSON")
	asJSON := f.Bool("json", false, "Print structured setup results")
	if err := f.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil
		}
		return err
	}
	if f.NArg() != 0 {
		return errors.New("setup takes flags only; use --client to select a client")
	}
	if *binary == "" {
		path, err := os.Executable()
		if err != nil {
			return err
		}
		path, err = filepath.EvalSymlinks(path)
		if err != nil {
			return err
		}
		*binary = path
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return err
	}
	configHome, err := os.UserConfigDir()
	if err != nil {
		return err
	}
	results, err := mcpconfig.Run(ctx, mcpconfig.Options{
		Client: *client, Executable: *binary, RunnerData: *data,
		Home: home, ConfigHome: configHome, OutputDir: *output, Apply: *apply,
	})
	if err != nil {
		return err
	}
	if *asJSON {
		if err := printJSON(results); err != nil {
			return err
		}
	} else {
		if !*apply {
			fmt.Println("Preview only. Run again with --apply to register the MCP launcher.")
		}
		for _, result := range results {
			fmt.Printf("%s: %s\n", result.Client, result.Status)
			if result.Path != "" {
				fmt.Println("  " + result.Path)
			}
			if result.Message != "" {
				fmt.Println("  " + result.Message)
			}
		}
	}
	for _, result := range results {
		switch result.Status {
		case "error", "conflict", "unsupported":
			return errors.New("some MCP clients could not be configured; see per-client results")
		}
	}
	return nil
}
