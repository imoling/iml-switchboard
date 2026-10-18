package core

import (
	"errors"
	"syscall"
	"unsafe"
)

// Windows：凭据管理器（advapi32 的 CredReadW / CredWriteW / CredDeleteW），存成「普通凭据」，只有当前用户能读。
// 目标名是「服务名:账号 id」，在「控制面板 → 凭据管理器 → Windows 凭据」里能看到和删除。
type wincredStore struct{}

var (
	advapi32     = syscall.NewLazyDLL("advapi32.dll")
	procRead     = advapi32.NewProc("CredReadW")
	procWrite    = advapi32.NewProc("CredWriteW")
	procDelete   = advapi32.NewProc("CredDeleteW")
	procCredFree = advapi32.NewProc("CredFree")
)

const (
	credTypeGeneric         = 1
	credPersistLocalMachine = 2
	errorNotFound           = 1168 // ERROR_NOT_FOUND
)

// credential 对应 Win32 的 CREDENTIALW。
type credential struct {
	Flags              uint32
	Type               uint32
	TargetName         *uint16
	Comment            *uint16
	LastWritten        syscall.Filetime
	CredentialBlobSize uint32
	CredentialBlob     *byte
	Persist            uint32
	AttributeCount     uint32
	Attributes         uintptr
	TargetAlias        *uint16
	UserName           *uint16
}

func platformSecretStore() secretStore {
	if advapi32.Load() != nil || procRead.Find() != nil {
		return nil
	}
	return wincredStore{}
}

func target(service, name string) (*uint16, error) {
	return syscall.UTF16PtrFromString(service + ":" + name)
}

func (wincredStore) get(service, name string) (string, error) {
	t, err := target(service, name)
	if err != nil {
		return "", err
	}
	var pcred *credential
	r, _, e := procRead.Call(uintptr(unsafe.Pointer(t)), credTypeGeneric, 0, uintptr(unsafe.Pointer(&pcred)))
	if r == 0 {
		if errno, ok := e.(syscall.Errno); ok && errno == errorNotFound {
			return "", errNoSecret
		}
		return "", errors.New("读取凭据管理器失败：" + e.Error())
	}
	defer procCredFree.Call(uintptr(unsafe.Pointer(pcred)))
	if pcred.CredentialBlobSize == 0 || pcred.CredentialBlob == nil {
		return "", errNoSecret
	}
	return string(unsafe.Slice(pcred.CredentialBlob, pcred.CredentialBlobSize)), nil
}

func (wincredStore) set(service, name, value string) error {
	t, err := target(service, name)
	if err != nil {
		return err
	}
	user, _ := syscall.UTF16PtrFromString(name)
	blob := []byte(value)
	c := credential{Type: credTypeGeneric, TargetName: t, UserName: user, Persist: credPersistLocalMachine, CredentialBlobSize: uint32(len(blob))}
	if len(blob) > 0 {
		c.CredentialBlob = &blob[0]
	}
	if r, _, e := procWrite.Call(uintptr(unsafe.Pointer(&c)), 0); r == 0 {
		return errors.New("写入凭据管理器失败：" + e.Error())
	}
	return nil
}

func (wincredStore) del(service, name string) error {
	t, err := target(service, name)
	if err != nil {
		return err
	}
	procDelete.Call(uintptr(unsafe.Pointer(t)), credTypeGeneric, 0)
	return nil
}
