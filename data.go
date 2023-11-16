package dockerdns

import (
	"fmt"
	"net"
	"strconv"

	dockerapi "github.com/fsouza/go-dockerclient"
)

// type ContainerData struct {
// 	id    string
// 	name  string
// 	ipv4  []net.IP
// 	ipv6  []net.IP
// 	hosts []string // the same as above
// }

var labels = []string{
	dockerHostLabel,
	dockerEnableLabel,
	dockerProjectLabel,
	dockerServiceLabel,
}

type ContainerData struct {
	name        string
	id          string
	hostname    string
	labeledHost string
	networks    []string
	// labeledNetwork string
	enabled       bool
	forceDisabled bool
	project       string
	service       string
	ipv4          []net.IP
	ipv6          []net.IP
	hosts         []string
}

func newContainerConfig(container *dockerapi.Container) *ContainerData {
	disabled := false
	enabled := false
	var err error
	val, ok := container.Config.Labels[dockerEnableLabel]
	if ok {
		enabled, err = strconv.ParseBool(val)
		if err == nil && !enabled {
			disabled = true
		}
	}

	return &ContainerData{
		labeledHost:   container.Config.Labels[dockerHostLabel],
		enabled:       enabled,
		forceDisabled: disabled,
		project:       container.Config.Labels[dockerProjectLabel],
		service:       container.Config.Labels[dockerServiceLabel],
	}
}

func (dd *DockerDiscovery) parseContainer(container *dockerapi.Container) (*ContainerData, error) {
	c := newContainerConfig(container)
	networks := []string{}
	for name := range container.NetworkSettings.Networks {
		if !dd.permittedNetwork(name) {
			continue
		}
		networks = append(networks, name)
	}
	c.networks = networks
	c.name = normalizeContainerName(container)
	c.id = container.ID
	c.hostname = container.Config.Hostname
	ipv4, ipv6, err := dd.getContainerAddresses(container)
	if err != nil {
		return c, err
	}
	if len(ipv4) == 0 && len(ipv6) == 0 {
		return c, nil
	}
	c.ipv4 = ipv4
	c.ipv6 = ipv6
	dd.resolveHosts(c)
	return c, nil
}

func (dd *DockerDiscovery) resolveHosts(c *ContainerData) {
	domains := make([]string, 0, 10)
	if dd.opts.byDomain && c.name != "" {
		domains = append(domains, c.name)
	}
	if dd.opts.byHostname && c.hostname != "" {
		domains = append(domains, c.hostname)
	}
	if dd.opts.byComposeDomain && c.service != "" && c.project != "" {
		domains = append(domains, c.service+"."+c.project)
	}
	if len(domains) != 0 {
		hosts := dd.makeFQDNs(domains)
		c.hosts = hosts
	}
	if c.labeledHost != "" {
		dd.addFQDN(c.labeledHost, c)
	}
}

func (dd *DockerDiscovery) addFQDN(name string, c *ContainerData) error {
	if name == "" {
		return fmt.Errorf("passed empty name")
	}
	name, err := dd.toFQDN(name)
	if err != nil {
		return err
	}
	if name == "" {
		return fmt.Errorf("fqdn must be not empty")
	}
	for i := range c.hosts {
		if c.hosts[i] == name {
			return nil
		}
	}
	c.hosts = append(c.hosts, name)
	return nil
}

func (dd *DockerDiscovery) permittedNetwork(network string) bool {
	if len(dd.opts.fromNetworks) == 0 {
		return true
	}
	for _, n := range dd.opts.fromNetworks {
		if n == network {
			return true
		}
	}
	return false
}
