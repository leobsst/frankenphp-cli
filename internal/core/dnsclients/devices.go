package dnsclients

import (
	"time"

	"github.com/leobsst/frankenphp-cli/internal/core/store"
)

// Device is a client of the LAN DNS server with its status, as
// `dns-clients --json` prints it.
type Device struct {
	store.DNSClient
	Status        string `json:"status"`
	RandomizedMAC bool   `json:"randomized_mac"`
}

// Devices returns the devices of stored at now, the connected ones only
// unless all, and how many are connected.
func Devices(stored []store.DNSClient, now time.Time, all bool) (devices []Device, connected int) {
	devices = []Device{}
	for _, c := range stored {
		status := Status(c, now)
		if status == Connected {
			connected++
		} else if !all {
			continue
		}
		devices = append(devices, Device{DNSClient: c, Status: status, RandomizedMAC: RandomizedMAC(c.MAC)})
	}
	return devices, connected
}
