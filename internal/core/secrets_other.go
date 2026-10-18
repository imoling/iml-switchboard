//go:build !darwin && !windows && !linux

package core

func platformSecretStore() secretStore { return nil }
