package dockerdns

import (
	"fmt"

	dockerapi "github.com/fsouza/go-dockerclient"

	"strings"

	"github.com/coredns/coredns/plugin"
)

func normalizeContainerName(container *dockerapi.Container) string {
	return strings.TrimLeft(container.Name, "/")
}

func (dd *DockerDiscovery) makeFQDNs(domains []string) []string {
	// make new host list: domain + zone from coredns block args
	// examples:
	// 1:
	// domains == ["one", "two"]
	// rzones == [".loc", ".docker.local"]
	// results are: ["one.loc", "one.docker.local", "two.loc", "two.docker.local"]
	// 2:
	// domains == ["one", "two"]
	// rzones == [""]
	// results are: ["one", "two"]
	// Note: FQDN always has trailing dot
	set := map[string]struct{}{}
	// if len(dd.rzones) == 0 {
	// 	return nil, fmt.Errorf("empty rzones list")
	// }
	dl := make([]string, 0, len(domains)*len(dd.rzones))
	for j := range domains {
		for k := range dd.rzones {
			name := domains[j] + dd.rzones[k]
			if name == "" {
				continue
			}
			name, err := dd.toFQDN(name)
			if err != nil {
				log.Errorf("[docker]  %s", err)
				continue
			}
			if _, ok := set[name]; ok {
				continue
			}
			dl = append(dl, name)
			set[name] = struct{}{}

		}
	}
	return dl
}

func (dd *DockerDiscovery) toFQDN(name string) (string, error) {
	name = plugin.Name(name).Normalize()
	if plugin.Zones(dd.Origins).Matches(name) == "" {
		return "", fmt.Errorf("name %s is not in Origins", name)
	}
	return name, nil
}
