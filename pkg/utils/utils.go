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
	hosts := []string{"google.com:80"}
	timeout := 2 * time.Second
	for _, host := range hosts {
		conn, err := net.DialTimeout("tcp", host, timeout)
		if err == nil {
			conn.Close()
			return true
		}
	}
	return false
}
