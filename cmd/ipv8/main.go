package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"time"

	"github.com/mryuvraj1/ipv8/internal/app"
)

const version = "0.1.0"

type agentConfig struct {
	Server string `json:"server"`
	ID     string `json:"id"`
	Secret string `json:"secret"`
}

func main() {
	if len(os.Args) < 2 {
		usage()
		os.Exit(2)
	}
	var err error
	switch os.Args[1] {
	case "serve":
		err = serve()
	case "bootstrap-admin":
		err = bootstrapAdmin()
	case "enroll":
		err = enroll(os.Args[2:])
	case "agent":
		err = runAgent(os.Args[2:])
	case "check-config":
		err = checkConfig(os.Args[2:])
	case "version", "--version", "-version":
		fmt.Printf("IPv8 experimental agent %s (%s/%s)\n", version, runtime.GOOS, runtime.GOARCH)
	default:
		usage()
		err = fmt.Errorf("unknown command %q", os.Args[1])
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "ipv8:", err)
		os.Exit(1)
	}
}

func usage() {
	fmt.Fprintln(os.Stderr, "usage: ipv8 serve | bootstrap-admin | enroll | agent | check-config | version")
}

func serve() error {
	addr := env("IPV8_ADDR", "127.0.0.1:8080")
	data := env("IPV8_DATA", "./data/ipv8.json")
	secure := strings.EqualFold(os.Getenv("IPV8_COOKIE_SECURE"), "true")
	if value := os.Getenv("IPV8_COOKIE_SECURE"); value != "" && !strings.EqualFold(value, "true") && !strings.EqualFold(value, "false") {
		return errors.New("IPV8_COOKIE_SECURE must be true or false")
	}
	a, err := app.New(data, secure)
	if err != nil {
		return err
	}
	s := &http.Server{Addr: addr, Handler: a.Handler(), ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 10 * time.Second, WriteTimeout: 15 * time.Second, IdleTimeout: 60 * time.Second, MaxHeaderBytes: 1 << 20}
	fmt.Printf("IPv8 panel listening on http://%s (experimental; local prototype only)\n", addr)
	return s.ListenAndServe()
}

func bootstrapAdmin() error {
	email, password := os.Getenv("IPV8_ADMIN_EMAIL"), os.Getenv("IPV8_ADMIN_PASSWORD")
	if email == "" || password == "" {
		return errors.New("set IPV8_ADMIN_EMAIL and IPV8_ADMIN_PASSWORD for this command")
	}
	a, err := app.New(env("IPV8_DATA", "./data/ipv8.json"), false)
	if err != nil {
		return err
	}
	if err := a.BootstrapAdmin(email, password); err != nil {
		return err
	}
	fmt.Println("Admin account created. Clear IPV8_ADMIN_PASSWORD from the environment.")
	return nil
}

func enroll(args []string) error {
	f := flag.NewFlagSet("enroll", flag.ContinueOnError)
	server := f.String("server", "", "IPv8 panel base URL")
	token := f.String("token", "", "short-lived one-time enrollment token")
	name := f.String("name", hostname(), "installation display name")
	configPath := f.String("config", "/etc/ipv8/agent.json", "agent configuration path")
	if err := f.Parse(args); err != nil {
		return err
	}
	if *token == "" || *name == "" {
		return errors.New("--token and --name are required")
	}
	if _, err := os.Lstat(*configPath); err == nil {
		return fmt.Errorf("configuration already exists at %s; it was not changed", *configPath)
	} else if !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("inspect configuration path: %w", err)
	}
	base, err := validServer(*server)
	if err != nil {
		return err
	}
	body, _ := json.Marshal(map[string]string{"token": *token, "name": *name, "platform": runtime.GOOS + "/" + runtime.GOARCH})
	req, err := http.NewRequest(http.MethodPost, base+"/api/installations/enroll", bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	client := agentHTTPClient(15 * time.Second)
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("enrollment request failed: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusCreated {
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 2048))
		return fmt.Errorf("enrollment failed (%s): %s", resp.Status, strings.TrimSpace(string(b)))
	}
	var answer struct{ ID, Secret string }
	if err := json.NewDecoder(io.LimitReader(resp.Body, 4096)).Decode(&answer); err != nil || answer.ID == "" || answer.Secret == "" {
		return errors.New("server returned an invalid enrollment response")
	}
	conf := agentConfig{Server: base, ID: answer.ID, Secret: answer.Secret}
	b, _ := json.MarshalIndent(conf, "", "  ")
	if err := os.MkdirAll(filepath.Dir(*configPath), 0700); err != nil {
		return err
	}
	fh, err := os.OpenFile(*configPath, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return fmt.Errorf("write config without overwriting an existing file: %w", err)
	}
	if _, err := fh.Write(b); err != nil {
		fh.Close()
		_ = os.Remove(*configPath)
		return err
	}
	if err := fh.Sync(); err != nil {
		fh.Close()
		_ = os.Remove(*configPath)
		return err
	}
	if err := fh.Close(); err != nil {
		_ = os.Remove(*configPath)
		return err
	}
	fmt.Printf("Installation %s enrolled; credentials saved with mode 0600.\n", answer.ID)
	return nil
}

func checkConfig(args []string) error {
	f := flag.NewFlagSet("check-config", flag.ContinueOnError)
	path := f.String("config", "/etc/ipv8/agent.json", "agent config path")
	if err := f.Parse(args); err != nil {
		return err
	}
	_, err := readAgentConfig(*path)
	return err
}

func runAgent(args []string) error {
	f := flag.NewFlagSet("agent", flag.ContinueOnError)
	path := f.String("config", "/etc/ipv8/agent.json", "agent config path")
	if err := f.Parse(args); err != nil {
		return err
	}
	cfg, err := readAgentConfig(*path)
	if err != nil {
		return err
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	client := agentHTTPClient(10 * time.Second)
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()
	for {
		if err := heartbeat(ctx, client, cfg); err != nil {
			if ctx.Err() != nil {
				return nil
			}
			if errors.Is(err, errAgentRevoked) {
				fmt.Fprintln(os.Stderr, "ipv8 agent enrollment was revoked; exiting")
				return nil
			}
			fmt.Fprintln(os.Stderr, "ipv8 agent heartbeat failed:", err)
		}
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
		}
	}
}

func heartbeat(ctx context.Context, client *http.Client, cfg agentConfig) error {
	b, _ := json.Marshal(map[string]string{"id": cfg.ID, "secret": cfg.Secret})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, cfg.Server+"/api/agent/heartbeat", bytes.NewReader(b))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		io.Copy(io.Discard, io.LimitReader(resp.Body, 2048))
		if resp.StatusCode == http.StatusUnauthorized {
			return errAgentRevoked
		}
		return fmt.Errorf("server returned %s", resp.Status)
	}
	return nil
}

func agentHTTPClient(timeout time.Duration) *http.Client {
	return &http.Client{
		Timeout: timeout,
		CheckRedirect: func(_ *http.Request, _ []*http.Request) error {
			// Enrollment tokens and installation secrets are sent in request bodies.
			// Never forward them to a redirect target on another origin.
			return http.ErrUseLastResponse
		},
	}
}

func readAgentConfig(path string) (agentConfig, error) {
	var cfg agentConfig
	info, err := os.Lstat(path)
	if err != nil {
		return cfg, fmt.Errorf("read agent config: %w", err)
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
		return cfg, errors.New("agent config must be a regular file, not a symlink")
	}
	if info.Mode().Perm()&0077 != 0 {
		return cfg, errors.New("agent config permissions must be 0600 or stricter")
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return cfg, err
	}
	if err := json.Unmarshal(b, &cfg); err != nil {
		return cfg, errors.New("agent config is invalid JSON")
	}
	if _, err := validServer(cfg.Server); err != nil || cfg.ID == "" || cfg.Secret == "" {
		return cfg, errors.New("agent config is missing a valid server, ID, or secret")
	}
	return cfg, nil
}

func validServer(raw string) (string, error) {
	u, err := url.Parse(strings.TrimRight(raw, "/"))
	if err != nil || (u.Scheme != "https" && u.Scheme != "http") || u.Host == "" || u.User != nil || (u.Path != "" && u.Path != "/") || u.RawQuery != "" || u.Fragment != "" {
		return "", errors.New("--server must be an HTTP(S) origin without credentials, query, or fragment")
	}
	if u.Scheme == "http" && u.Hostname() != "localhost" && u.Hostname() != "127.0.0.1" && u.Hostname() != "::1" {
		return "", errors.New("agent enrollment requires HTTPS except for localhost prototypes")
	}
	return strings.TrimRight(u.String(), "/"), nil
}

var errAgentRevoked = errors.New("agent enrollment revoked")

func hostname() string {
	h, err := os.Hostname()
	if err != nil || h == "" {
		return "IPv8 installation"
	}
	return h
}
func env(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}
