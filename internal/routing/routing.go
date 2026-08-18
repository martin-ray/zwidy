package routing

import (
	"os/exec"
)

func AddRoute(cidr, iface string) error {
	return exec.Command("ip", "route", "replace", cidr, "dev", iface).Run()
	}

func DeleteRoute(cidr, iface string) error {
	return exec.Command("ip", "route", "del", cidr, "dev", iface).Run()
	}
