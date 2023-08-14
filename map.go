package dockerdiscovery

import (
	"net"

	csm "github.com/mhmtszr/concurrent-swiss-map"
)

type Map struct {
	name4 *csm.CsMap[string, []net.IP] // [host, ipv4]
	name6 *csm.CsMap[string, []net.IP] // [host, ipv6]

	ids *csm.CsMap[string, *ContainerData] // [container_id, container_info]

	// Key for the list of host names must be a literal IP address
	// including IPv6 address without zone identifier.
	// We don't support old-classful IP address notation.
	addr        *csm.CsMap[string, []string]
	autoReverse *bool
}

func newCSMap() *csm.CsMap[string, []net.IP] {
	return csm.Create[string, []net.IP](
		csm.WithShardCount[string, []net.IP](32),
		csm.WithSize[string, []net.IP](100),
	)
}

func (m *Map) addContainer(info *ContainerData) {
	m.ids.Store(info.id, info)
	for i := range info.hosts {
		if len(info.ipv4) != 0 {
			m.name4.Store(info.hosts[i], info.ipv4)
		}
		if len(info.ipv6) != 0 {
			m.name6.Store(info.hosts[i], info.ipv6)
		}
	}
	if *m.autoReverse {
		m.addAddrs(info)
	}
}

func (m *Map) addAddrs(info *ContainerData) {
	add := func(ips []net.IP, hosts []string) {
		for i := range ips {
			names, ok := m.addr.Load(string(ips[i]))
			if ok {
				names = append(names, hosts...)
			} else {
				names = hosts
			}
			m.addr.Store(string(ips[i]), names)
		}
	}
	add(info.ipv4, info.hosts)
	add(info.ipv6, info.hosts)
}

func (m *Map) removeContainer(id string) {
	info, ok := m.ids.Load(id)
	if !ok {
		return
	}
	m.ids.Delete(info.id)
	for i := range info.hosts {
		m.name4.Delete(info.hosts[i])
		m.name6.Delete(info.hosts[i])
	}
	m.rmAddrs(info)
}

func (m *Map) rmAddrs(info *ContainerData) {
	rm := func(ips []net.IP) {
		for i := range ips {
			m.addr.Delete(string(ips[i]))
		}
	}
	rm(info.ipv4)
	rm(info.ipv6)
}
