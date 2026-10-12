//go:build windows
package xray

import ("os/exec";"syscall")

// The proxy core must not be owned by a kill-on-close Job Object.
func attachChildLifetime(cmd *exec.Cmd){
 if cmd==nil{return}
 cmd.SysProcAttr=&syscall.SysProcAttr{CreationFlags:syscall.CREATE_NEW_PROCESS_GROUP|syscall.DETACHED_PROCESS,HideWindow:true}
}
