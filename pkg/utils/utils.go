package utils

import (
	"log/slog"
	"net"
	"time"
)

func Err(err error) slog.Attr {
	return slog.Attr{
		Key:   "error",
		Value: slog.StringValue(err.Error()),
	}
}

func IsNetworkAvailable() bool {
	timeout := 2 * time.Second
	conn, err := net.DialTimeout("tcp", "8.8.8.8:53", timeout)
	if err != nil {
		return false
	}
	conn.Close()
	return true
}
