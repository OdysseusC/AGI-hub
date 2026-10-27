package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"reflect"
	"strings"
	"syscall"
	"time"

	"agihub/internal/app"
	"agihub/internal/autostart"
)

func runAutostart(args []string) error {
	if len(args) == 0 || args[0] == "--help" || args[0] == "help" {
		fmt.Println("可选登录自启动（默认关闭） / Optional user-login startup\nUsage: agi-hub autostart <status|enable|disable|register-client|remove-client|run> --data <private-directory>\nregister-client: --endpoint <id> --file <absolute-launch-JSON>\nenable: [--binary <installed-Hub>] [--clients <comma-separated-registered-IDs>]\nChanges apply at next user login; existing processes are not stopped. Desktop hosts still require their native environment.")
		return nil
	}
	fs := flag.NewFlagSet("autostart "+args[0], flag.ContinueOnError)
	dir := fs.String("data", app.DefaultDataDir(), "private data directory")
	binary := fs.String("binary", "", "absolute installed Hub binary")
	endpoint := fs.String("endpoint", "", "registered local endpoint")
	file := fs.String("file", "", "absolute Client launch JSON (no tokens or host IPC)")
	clients := fs.String("clients", "", "comma-separated startup Clients; empty selects Hub only")
	if err := fs.Parse(args[1:]); err != nil {
		return err
	}
	if fs.NArg() != 0 {
		return errors.New("unexpected startup arguments")
	}
	abs, err := filepath.Abs(*dir)
	if err != nil {
		return err
	}
	*dir = abs
	if _, err := app.LoadConfig(*dir); err != nil {
		return err
	}
	switch args[0] {
	case "status":
		v, err := app.StartupStatus(*dir)
		if err != nil {
			return err
		}
		return output(v)
	case "enable", "disable":
		var selected []string
		fs.Visit(func(f *flag.Flag) {
			if f.Name == "clients" {
				selected = []string{}
				if *clients != "" {
					selected = strings.Split(*clients, ",")
				}
			}
		})
		if args[0] == "enable" && *binary == "" {
			p, e := autostart.Load(*dir)
			if e != nil {
				return e
			}
			*binary = p.Binary
			if *binary == "" {
				*binary, err = os.Executable()
				if err != nil {
					return err
				}
			}
		}
		if err := app.ConfigureStartup(*dir, *binary, args[0] == "enable", selected); err != nil {
			return err
		}
		v, err := app.StartupStatus(*dir)
		if err != nil {
			return err
		}
		return output(v)
	case "register-client":
		if *endpoint == "" || !filepath.IsAbs(*file) {
			return errors.New("register-client requires --endpoint and an absolute --file")
		}
		info, err := os.Lstat(*file)
		if err != nil {
			return err
		}
		if !info.Mode().IsRegular() || info.Size() > 1<<20 {
			return errors.New("launch JSON must be a regular file under 1 MiB")
		}
		f, err := os.Open(*file)
		if err != nil {
			return err
		}
		defer f.Close()
		var c autostart.Client
		d := json.NewDecoder(f)
		d.DisallowUnknownFields()
		if err := d.Decode(&c); err != nil {
			return err
		}
		if err := d.Decode(new(any)); err != io.EOF {
			return errors.New("launch file must contain exactly one JSON object")
		}
		if c.Endpoint != "" && c.Endpoint != *endpoint {
			return errors.New("launch endpoint differs from --endpoint")
		}
		c.Endpoint = *endpoint
		if err := app.RegisterStartupClient(*dir, c); err != nil {
			return err
		}
		return output(map[string]any{"registered": c.Endpoint, "started": false, "detail": "已登记；默认不选择此 Client。请在管理页或 enable --clients 中选择。"})
	case "remove-client":
		if *endpoint == "" {
			return errors.New("remove-client requires --endpoint")
		}
		return autostart.Remove(*dir, *endpoint)
	case "run":
		plan, err := autostart.Load(*dir)
		if err != nil {
			return err
		}
		cfg, err := app.LoadConfig(*dir)
		if err != nil {
			return err
		}
		ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
		defer stop()
		probe := func(ctx context.Context) error { return probeStartupHub(ctx, cfg) }
		gate := func(c autostart.Client) error {
			latest, err := autostart.Load(*dir)
			if err != nil {
				return err
			}
			selected := false
			for _, registered := range latest.Clients {
				if registered.Endpoint == c.Endpoint && registered.Enabled && reflect.DeepEqual(registered, c) {
					selected = true
				}
			}
			if !latest.Enabled || !selected {
				return errors.New("Client startup selection changed; explicit review required before restarting")
			}
			current, err := app.LoadConfig(*dir)
			if err != nil {
				return err
			}
			for _, b := range current.Bindings {
				if b.Endpoint.ID == c.Endpoint {
					if b.Endpoint.Driver == "codex" || b.Endpoint.Driver == "claude-code" {
						return errors.New("native desktop/interactive host is required")
					}
					if !b.Endpoint.Enabled || b.Endpoint.SessionID == "" {
						return errors.New("endpoint is disabled or not bound")
					}
					return nil
				}
			}
			return errors.New("endpoint is no longer configured")
		}
		return autostart.Run(ctx, *dir, plan, autostart.RunOptions{Probe: probe, ClientGate: gate})
	}
	return errors.New("unknown autostart operation")
}

func probeStartupHub(ctx context.Context, cfg app.Config) error {
	transport := &http.Transport{Proxy: nil, DialContext: (&net.Dialer{Timeout: time.Second}).DialContext}
	defer transport.CloseIdleConnections()
	req, err := http.NewRequestWithContext(ctx, "GET", "http://"+cfg.Listen+"/v1/health", nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+cfg.AdminToken)
	response, err := (&http.Client{Transport: transport, Timeout: 2 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}).Do(req)
	if err != nil {
		if connectionRefused(err) {
			return autostart.ErrHubUnavailable
		}
		return errors.New("local Hub readiness could not be verified")
	}
	defer response.Body.Close()
	if response.StatusCode != 200 {
		return errors.New("local Hub authentication or readiness failed")
	}
	var health struct {
		Service  string `json:"service"`
		Ready    bool   `json:"ready"`
		DeviceID string `json:"device_id"`
	}
	d := json.NewDecoder(io.LimitReader(response.Body, 4096))
	if err := d.Decode(&health); err != nil {
		return errors.New("unexpected local Hub readiness response")
	}
	if err := d.Decode(new(any)); err != io.EOF {
		return errors.New("unexpected trailing local Hub readiness response")
	}
	if health.Service != "agi-hub" || !health.Ready || health.DeviceID != cfg.DeviceID {
		return errors.New("local listener is not the configured ready Hub")
	}
	return nil
}
