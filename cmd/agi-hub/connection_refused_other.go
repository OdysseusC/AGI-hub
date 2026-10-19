//go:build !windows

package main

import (
	"errors"
	"syscall"
)

func connectionRefused(err error) bool {
	return errors.Is(err, syscall.ECONNREFUSED)
}
