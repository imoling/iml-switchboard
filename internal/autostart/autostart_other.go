//go:build !darwin && !windows && !linux

package autostart

import "errors"

func Install(exe string, logDir string) error {
	return errors.New("这个系统还不支持开机自启")
}
func Uninstall() error { return nil }
func Installed() bool  { return false }
