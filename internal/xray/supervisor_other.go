//go:build !linux && !windows
package xray
import "errors"
func independentCoreEnabled()bool{return false}
func(p *process)startIndependentCore(_ string,_,_ []byte)error{return errors.New("standalone core unavailable on this OS")}
func stopIndependentCore()error{return errors.New("standalone core unavailable on this OS")}
