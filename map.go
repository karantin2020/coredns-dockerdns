package dockerdiscovery

import (
	"net"

	csm "github.com/mhmtszr/concurrent-swiss-map"
)

type Map struct {
	name4 *csm.CsMap[string, []net.IP] // [host, ipv4]
	name6 *csm.CsMap[string, []net.IP] // [host, ipv6]

	ids *csm.CsMap[string, *ContainerData] // [container_id, container_info]
}

func newCSMap() *csm.CsMap[string, []net.IP] {
	return csm.Create[string, []net.IP](
		csm.WithShardCount[string, []net.IP](32),
		csm.WithSize[string, []net.IP](100),
	)
}

func (dd *Map) addContainer(info *ContainerData) {
	dd.ids.Store(info.id, info)
	for i := range info.hosts {
		if len(info.ipv4) != 0 {
			dd.name4.Store(info.hosts[i], info.ipv4)
		}
		if len(info.ipv6) != 0 {
			dd.name6.Store(info.hosts[i], info.ipv6)
		}
	}
}

func (dd *Map) removeContainer(id string) {
	info, ok := dd.ids.Load(id)
	if !ok {
		return
	}
	dd.ids.Delete(info.id)
	for i := range info.hosts {
		dd.name4.Delete(info.hosts[i])
		dd.name6.Delete(info.hosts[i])
	}
}
