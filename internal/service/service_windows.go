//go:build windows

// Package service runs Printty in the background on Windows: as a Windows
// service (starts at boot, restarts on failure) or at sign-in (no admin).
package service

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/registry"
	"golang.org/x/sys/windows/svc"
	"golang.org/x/sys/windows/svc/mgr"
)

const (
	Name        = "Printty"
	displayName = "Printty print server"
	description = "Shares this computer's printer with phones on the local network."
	runKey      = `Software\Microsoft\Windows\CurrentVersion\Run`
	firewall    = "Printty"
	trayValue   = "Printty tray"
)

// Supported reports whether this OS has service and autostart support.
const Supported = true

// ErrCancelled means the person declined the Windows permission prompt.
var ErrCancelled = errors.New("Windows permission was not given")

// IsService reports whether this process was started by the Service Control Manager.
func IsService() bool {
	ok, err := svc.IsWindowsService()
	return err == nil && ok
}

// State describes how Printty is set up to start.
type State struct {
	Installed bool // a Windows service exists
	Running   bool // the service is running
	AtSignIn  bool // the sign-in Run entry exists for this user
}

func Status() State {
	st := statusOf(Name)
	st.AtSignIn = AutostartEnabled()
	return st
}

// statusOf asks with the least access, so it works without administrator rights.
func statusOf(name string) State {
	var st State
	scm, err := windows.OpenSCManager(nil, nil, windows.SC_MANAGER_CONNECT)
	if err != nil {
		return st
	}
	defer windows.CloseServiceHandle(scm)
	h, err := windows.OpenService(scm, windows.StringToUTF16Ptr(name), windows.SERVICE_QUERY_STATUS)
	if err != nil {
		return st
	}
	defer windows.CloseServiceHandle(h)
	st.Installed = true
	var ss windows.SERVICE_STATUS
	if windows.QueryServiceStatus(h, &ss) == nil {
		st.Running = ss.CurrentState == windows.SERVICE_RUNNING || ss.CurrentState == windows.SERVICE_START_PENDING
	}
	return st
}

// Install registers the service (automatic, delayed start, restart on failure)
// and opens the port on Private networks. Needs administrator rights. If the
// service already exists (an upgrade, or a new folder) it is updated in place.
func Install(exe string, args []string, port string) error {
	m, err := mgr.Connect()
	if err != nil {
		return fmt.Errorf("connect to Windows services: %w", err)
	}
	defer m.Disconnect()
	conf := mgr.Config{
		DisplayName:      displayName,
		Description:      description,
		StartType:        mgr.StartAutomatic,
		DelayedAutoStart: true,
	}
	s, err := m.OpenService(Name)
	if err == nil {
		old, cerr := s.Config()
		if cerr != nil {
			s.Close()
			return fmt.Errorf("read service settings: %w", cerr)
		}
		conf.ServiceType, conf.ErrorControl = old.ServiceType, old.ErrorControl
		conf.ServiceStartName = old.ServiceStartName
		conf.BinaryPathName = Quote(append([]string{exe}, args...))
		if err := s.UpdateConfig(conf); err != nil {
			s.Close()
			return fmt.Errorf("update service: %w", err)
		}
	} else {
		s, err = m.CreateService(Name, exe, conf, args...)
		if err != nil {
			return fmt.Errorf("create service: %w", err)
		}
	}
	defer s.Close()
	s.SetRecoveryActions([]mgr.RecoveryAction{
		{Type: mgr.ServiceRestart, Delay: 10 * time.Second},
		{Type: mgr.ServiceRestart, Delay: 30 * time.Second},
		{Type: mgr.ServiceRestart, Delay: 2 * time.Minute},
	}, uint32((24 * time.Hour).Seconds()))
	if err := allowUserControl(s.Handle); err != nil {
		return fmt.Errorf("let users start and stop the service: %w", err)
	}
	openFirewall(exe, port)
	trayArgs := []string{exe, "tray"}
	for i := 0; i+1 < len(args); i += 2 {
		if args[i] == "-addr" {
			trayArgs = append(trayArgs, "-addr", args[i+1])
		}
	}
	return setTrayAutostart(Quote(trayArgs))
}

// Uninstall stops and removes the service and its firewall rule.
func Uninstall() error { return remove(true) }

// RemoveSelf deletes the service registration and firewall rule without
// stopping it, for a service removing itself (it then exits on its own).
func RemoveSelf() error { return remove(false) }

func remove(stop bool) error {
	m, err := mgr.Connect()
	if err != nil {
		return fmt.Errorf("connect to Windows services: %w", err)
	}
	defer m.Disconnect()
	s, err := m.OpenService(Name)
	if err != nil {
		return fmt.Errorf("the Printty service isn't installed")
	}
	defer s.Close()
	if stop {
		s.Control(svc.Stop)
	}
	if err := s.Delete(); err != nil {
		return fmt.Errorf("remove service: %w", err)
	}
	netsh("advfirewall", "firewall", "delete", "rule", "name="+firewall)
	removeTrayAutostart()
	return nil
}

// Start, Stop and Restart need no administrator rights once the service is
// installed: Install lets signed-in users start and stop it (from the tray).
func Start() error {
	return withService(windows.SERVICE_START, func(h windows.Handle) error {
		return windows.StartService(h, 0, nil)
	})
}

func Stop() error {
	return withService(windows.SERVICE_STOP|windows.SERVICE_QUERY_STATUS, func(h windows.Handle) error {
		var ss windows.SERVICE_STATUS
		err := windows.ControlService(h, windows.SERVICE_CONTROL_STOP, &ss)
		if errors.Is(err, windows.ERROR_SERVICE_NOT_ACTIVE) {
			return nil
		}
		return err
	})
}

// Restart stops the service, waits for it to stop, and starts it again.
func Restart() error {
	if err := Stop(); err != nil {
		return err
	}
	for i := 0; i < 40 && statusOf(Name).Running; i++ {
		time.Sleep(500 * time.Millisecond)
	}
	return Start()
}

func withService(access uint32, do func(h windows.Handle) error) error {
	scm, err := windows.OpenSCManager(nil, nil, windows.SC_MANAGER_CONNECT)
	if err != nil {
		return fmt.Errorf("connect to Windows services: %w", err)
	}
	defer windows.CloseServiceHandle(scm)
	h, err := windows.OpenService(scm, windows.StringToUTF16Ptr(Name), access)
	if errors.Is(err, windows.ERROR_SERVICE_DOES_NOT_EXIST) {
		return fmt.Errorf("the Printty service isn't installed")
	}
	if err != nil {
		return err
	}
	defer windows.CloseServiceHandle(h)
	return do(h)
}

// IsAccessDenied reports whether err means Windows wants administrator rights.
func IsAccessDenied(err error) bool { return errors.Is(err, windows.ERROR_ACCESS_DENIED) }

// userControl is the default service security plus start (RP) and stop (WP)
// for interactive users (IU), so the tray can control Printty without a prompt.
const userControl = "D:(A;;CCLCSWRPWPDTLOCRRC;;;SY)(A;;CCDCLCSWRPWPDTLOCRSDRCWDWO;;;BA)(A;;CCLCSWRPWPLOCRRC;;;IU)(A;;CCLCSWLOCRRC;;;SU)"

func allowUserControl(h windows.Handle) error {
	sd, err := windows.SecurityDescriptorFromString(userControl)
	if err != nil {
		return err
	}
	r, _, e := procSetServiceObjectSecurity.Call(uintptr(h), uintptr(windows.DACL_SECURITY_INFORMATION), uintptr(unsafe.Pointer(sd)))
	if r == 0 {
		return e
	}
	return nil
}

var procSetServiceObjectSecurity = windows.NewLazySystemDLL("advapi32.dll").NewProc("SetServiceObjectSecurity")

// ---- tray icon for every user (the service itself can't show one)

// setTrayAutostart writes the HKLM Run entry that starts "printty tray" at each sign-in.
func setTrayAutostart(command string) error {
	k, _, err := registry.CreateKey(registry.LOCAL_MACHINE, runKey, registry.SET_VALUE)
	if err != nil {
		return err
	}
	defer k.Close()
	return k.SetStringValue(trayValue, command)
}

func removeTrayAutostart() {
	if k, err := registry.OpenKey(registry.LOCAL_MACHINE, runKey, registry.SET_VALUE); err == nil {
		k.DeleteValue(trayValue)
		k.Close()
	}
}

func netsh(args ...string) error {
	cmd := exec.Command("netsh", args...)
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: 0x08000000}
	return cmd.Run()
}

func openFirewall(exe, port string) {
	netsh("advfirewall", "firewall", "delete", "rule", "name="+firewall)
	netsh("advfirewall", "firewall", "add", "rule", "name="+firewall, "dir=in", "action=allow",
		"program="+exe, "protocol=TCP", "localport="+port, "profile=private", "description="+description)
}

// Run hands control to the Service Control Manager. run gets a context that
// ends when Windows asks the service to stop.
func Run(run func(ctx context.Context) error) error {
	return svc.Run(Name, &handler{run: run})
}

type handler struct {
	run func(ctx context.Context) error
}

func (h *handler) Execute(args []string, req <-chan svc.ChangeRequest, status chan<- svc.Status) (bool, uint32) {
	status <- svc.Status{State: svc.StartPending}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- h.run(ctx) }()
	status <- svc.Status{State: svc.Running, Accepts: svc.AcceptStop | svc.AcceptShutdown}
	for {
		select {
		case err := <-done:
			cancel()
			if err != nil {
				return true, 1
			}
			return false, 0
		case c := <-req:
			switch c.Cmd {
			case svc.Interrogate:
				status <- c.CurrentStatus
			case svc.Stop, svc.Shutdown:
				status <- svc.Status{State: svc.StopPending}
				cancel()
				select {
				case <-done:
				case <-time.After(10 * time.Second):
				}
				return false, 0
			}
		}
	}
}

// ---- start at sign-in (per user, no admin)

func EnableAutostart(command string) error {
	k, _, err := registry.CreateKey(registry.CURRENT_USER, runKey, registry.SET_VALUE)
	if err != nil {
		return err
	}
	defer k.Close()
	return k.SetStringValue(Name, command)
}

func DisableAutostart() error {
	k, err := registry.OpenKey(registry.CURRENT_USER, runKey, registry.SET_VALUE)
	if err != nil {
		return err
	}
	defer k.Close()
	if err := k.DeleteValue(Name); err != nil && !errors.Is(err, registry.ErrNotExist) {
		return err
	}
	return nil
}

func AutostartEnabled() bool {
	k, err := registry.OpenKey(registry.CURRENT_USER, runKey, registry.QUERY_VALUE)
	if err != nil {
		return false
	}
	defer k.Close()
	_, _, err = k.GetStringValue(Name)
	return err == nil
}

// Quote builds a command line from arguments.
func Quote(args []string) string {
	q := make([]string, len(args))
	for i, a := range args {
		q[i] = syscall.EscapeArg(a)
	}
	return strings.Join(q, " ")
}

// ---- elevation

type shellExecuteInfo struct {
	Size          uint32
	Mask          uint32
	Hwnd          windows.Handle
	Verb          *uint16
	File          *uint16
	Parameters    *uint16
	Directory     *uint16
	Show          int32
	InstApp       windows.Handle
	IDList        uintptr
	Class         *uint16
	KeyClass      windows.Handle
	HotKey        uint32
	IconOrMonitor windows.Handle
	Process       windows.Handle
}

var procShellExecuteExW = windows.NewLazySystemDLL("shell32.dll").NewProc("ShellExecuteExW")

// Elevate starts exe with args as administrator (Windows shows a permission
// prompt on this PC's screen). It returns once the process has started.
func Elevate(exe string, args []string) error {
	const seeMaskNoCloseProcess = 0x40
	info := shellExecuteInfo{
		Mask:       seeMaskNoCloseProcess,
		Verb:       windows.StringToUTF16Ptr("runas"),
		File:       windows.StringToUTF16Ptr(exe),
		Parameters: windows.StringToUTF16Ptr(Quote(args)),
		Directory:  windows.StringToUTF16Ptr(filepath.Dir(exe)),
		Show:       0, // SW_HIDE
	}
	info.Size = uint32(unsafe.Sizeof(info))
	r, _, err := procShellExecuteExW.Call(uintptr(unsafe.Pointer(&info)))
	if r == 0 {
		if errors.Is(err, windows.ERROR_CANCELLED) {
			return ErrCancelled
		}
		return fmt.Errorf("couldn't ask for administrator rights: %w", err)
	}
	windows.CloseHandle(info.Process)
	return nil
}

// IsAdmin reports whether this process has administrator rights.
func IsAdmin() bool {
	return windows.GetCurrentProcessToken().IsElevated()
}

// HideConsole detaches from the console window (for sign-in autostart).
var procFreeConsole = windows.NewLazySystemDLL("kernel32.dll").NewProc("FreeConsole")

func HideConsole() {
	procFreeConsole.Call()
}

// ExeDir is the folder holding printty.exe.
func ExeDir() string {
	exe, err := os.Executable()
	if err != nil {
		return "."
	}
	return filepath.Dir(exe)
}
