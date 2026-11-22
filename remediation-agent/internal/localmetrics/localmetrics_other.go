//go:build !linux

// Package localmetrics is only implemented for Linux.
package localmetrics

import (
	"context"
	"errors"
)

func Read(context.Context, string, string) (float64, error) {
	return 0, errors.New("local metrics need linux")
}

func Supported(string) bool { return false }
