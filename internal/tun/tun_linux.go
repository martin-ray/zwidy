//go:build linux

package tun

import (
	"fmt"
	"os"
	"os/exec"
	"unsafe"

	"syscall"
)

const (
	ifrSize   = 40
	iFFTun    = 0x0001
	iFFNoPI   = 0x1000
	tunSetIFF = 0x400454ca
)

type Device struct {
	File  *os.File
	Name  string
	CIDR  string
	MTU   int
	Local string
}

func Open(name, localCIDR string, mtu int) (*Device, error) {
	f, err := os.OpenFile("/dev/net/tun", os.O_RDWR, 0)
	if err != nil {
		return nil, err
	}
	var ifr [ifrSize]byte
	copy(ifr[:], []byte(name))
	*(*uint16)(unsafe.Pointer(&ifr[16])) = iFFTun | iFFNoPI
	if _, _, errno := syscall.Syscall(syscall.SYS_IOCTL, f.Fd(), uintptr(tunSetIFF), uintptr(unsafe.Pointer(&ifr[0]))); errno != 0 {
		_ = f.Close()
		return nil, errno
	}
	actualName := string(ifr[:16])
	for i := range actualName {
		if actualName[i] == 0 {
			actualName = actualName[:i]
			break
		}
	}
	dev := &Device{File: f, Name: actualName, CIDR: localCIDR, MTU: mtu}
	if err := dev.configure(); err != nil {
		_ = f.Close()
		return nil, err
	}
	return dev, nil
	}

func (d *Device) configure() error {
	if err := exec.Command("ip", "link", "set", "dev", d.Name, "mtu", fmt.Sprintf("%d", d.MTU)).Run(); err != nil {
		return err
	}
	if err := exec.Command("ip", "addr", "replace", d.CIDR, "dev", d.Name).Run(); err != nil {
		return err
	}
	return exec.Command("ip", "link", "set", "dev", d.Name, "up").Run()
	}

func (d *Device) Read(p []byte) (int, error) { return d.File.Read(p) }
func (d *Device) Write(p []byte) (int, error) { return d.File.Write(p) }

func (d *Device) Close() error {
	_ = exec.Command("ip", "link", "del", d.Name).Run()
	return d.File.Close()
	}
