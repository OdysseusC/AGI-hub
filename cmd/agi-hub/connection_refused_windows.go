package main

import (
	"errors"
	"syscall"
)

// Winsock reports WSAECONNREFUSED, rather than syscall's synthetic POSIX
// ECONNREFUSED. Only this exact connection-refused code proves Hub absence.
const wsaConnectionRefused = syscall.Errno(10061)

func connectionRefused(err error) bool {
	return errors.Is(err, syscall.ECONNREFUSED) || errors.Is(err, wsaConnectionRefused)
}
