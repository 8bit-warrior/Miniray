//go:build !linux && !windows

package xray

import "errors"

func independentCoreEnabled() bool { return false }
func IndependentCoreManuallyStopped() bool { return false }
func SetIndependentCoreManuallyStopped(bool) error { return nil }
func StartIndependentAgent() error { return nil }

func IndependentCoreRunning() bool { return false }
func StopIndependentCore() error {
	return errors.New("standalone core unavailable on this OS")
}
func (p *process) startIndependentCore(_ string, _, _ []byte, _ bool) error {
	return errors.New("standalone core unavailable on this OS")
}
func stopIndependentCore() error { return errors.New("standalone core unavailable on this OS") }
