//go:build !linux && !windows

package service

import (
	"context"
	"errors"
	"os"

	"powerhook/internal/config"
)

const AdminHint = "check file permissions"

var errUnsupported = errors.New("only supported on Linux and Windows")

func IsService() bool                           { return false }
func Run(func(context.Context) error) error     { return errUnsupported }
func OpenLog() (*os.File, error)                { return nil, errUnsupported }
func RequireAdmin(string) error                 { return nil }
func RestartIfRunning() (bool, error)           { return false, nil }
func Restart() (bool, error)                    { return false, nil }
func FirewallBackend() string                   { return "" }
func FirewallCurrent(*config.Config) bool       { return false }
func OpenFirewall(*config.Config) error         { return errUnsupported }
func CloseFirewall() error                      { return nil }
func FirewallInstalled() string                 { return "" }
func installedComponents() ([]component, error) { return nil, errUnsupported }
