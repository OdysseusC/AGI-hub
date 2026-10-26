//go:build darwin || linux

package autostart

import "os"

func replaceStartupJSONFile(staged, destination string, _ bool) error {
	return os.Rename(staged, destination)
}
