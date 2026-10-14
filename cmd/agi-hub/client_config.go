package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"agihub/internal/app"
)

// Write scoped credentials directly to a new private file, never stdout.
func writeClientConfig(cfg app.Config, endpoint, path string) (string, error) {
	if endpoint == "" || !filepath.IsAbs(path) {
		return "", errors.New("client-config requires --endpoint and an absolute --file path outside the code repository")
	}
	found := false
	for _, b := range cfg.Bindings {
		if b.Endpoint.ID == endpoint {
			found = true
			break
		}
	}
	if !found {
		return "", errors.New("client-config endpoint is not registered in the local configuration")
	}
	path = filepath.Clean(path)
	// Check existing ancestors before creating directories: a symlink can redirect
	// a credential file into an unintended location even with O_EXCL on the file.
	for current := filepath.Dir(path); ; current = filepath.Dir(current) {
		info, err := os.Lstat(current)
		if err != nil && !os.IsNotExist(err) {
			return "", err
		}
		if err == nil && (!info.IsDir() || info.Mode()&os.ModeSymlink != 0) {
			return "", fmt.Errorf("private config parent must be a real directory: %s", current)
		}
		if _, gitErr := os.Lstat(filepath.Join(current, ".git")); gitErr == nil {
			return "", errors.New("private client config must be outside a Git checkout")
		} else if !os.IsNotExist(gitErr) {
			return "", gitErr
		}
		if filepath.Dir(current) == current {
			break
		}
	}
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return "", err
	}
	payload := struct {
		BaseURL       string         `json:"base_url"`
		Endpoint      string         `json:"endpoint"`
		EndpointToken string         `json:"endpoint_token"`
		Native        map[string]any `json:"native"`
	}{"http://" + cfg.Listen, endpoint, app.EndpointToken(cfg.AdminToken, endpoint), map[string]any{}}
	data, err := json.MarshalIndent(payload, "", "  ")
	if err != nil {
		return "", err
	}
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return "", fmt.Errorf("create private client config without overwriting: %w", err)
	}
	_, err = f.Write(append(data, '\n'))
	if err == nil {
		err = f.Sync()
	}
	closeErr := f.Close()
	if err != nil {
		return "", err
	}
	return path, closeErr
}
