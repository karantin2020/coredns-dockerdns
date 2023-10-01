package dockerdiscovery

import (
	"context"
	"net"

	"github.com/docker/docker/api/types/swarm"
	dockerapi "github.com/fsouza/go-dockerclient"
)

// type SwarmProvider struct {
// 	dd *DockerDiscovery
// }

func (dd *DockerDiscovery) scanServices() error {
	log.Info("[docker] Scan services")
	ls, err := dd.listServices()
	if err != nil {
		return err
	}
	for _, s := range ls {
		err := dd.updateService(s)
		if err != nil {
			return err
		}
	}
	return nil
}

func (dd *DockerDiscovery) listServices() ([]*swarm.Service, error) {
	pservices := []*swarm.Service{}
	// If swarm inspect does not return error it means that swarm mode is enabled.
	_, err := dd.dockerClient.InspectSwarm(context.Background())
	if err != nil {
		return nil, err
	}

	services, err := dd.dockerClient.ListServices(dockerapi.ListServicesOptions{})
	if err != nil {
		return nil, err
	}

	for i := range services {
		service, err := dd.dockerClient.InspectService(services[i].ID)
		if err != nil {
			log.Errorf("[docker] Inspect service %s: %s", services[i].ID[:12], err)
			continue
		}
		pservices = append(pservices, service)
	}
	return pservices, nil
}

func (dd *DockerDiscovery) parseService(service *swarm.Service) (ContainerData, error) {
	dData := ContainerData{
		id: service.ID,
	}
	serviceName := service.Spec.Name

	extractLabels(service.Spec.TaskTemplate.ContainerSpec.Labels, &dData)
	dData.project = service.Spec.TaskTemplate.ContainerSpec.Labels["com.docker.stack.namespace"]
	if len(serviceName) > len(dData.project)+1 {
		dData.name = serviceName[len(dData.project)+1:]
		dData.service = dData.name
	}
	serviceVirtualIPs(service, &dData.ipv4, &dData.ipv6)
	log.Infof("[docker] Parse service %s (%s)", dData.name, dData.id[:12])

	taskFilters := make(map[string][]string)
	taskFilters["service"] = []string{service.Spec.Name}
	taskFilters["desired-state"] = []string{"running"}
	tasks, err := dd.dockerClient.ListTasks(dockerapi.ListTasksOptions{
		Filters: taskFilters,
	})
	if err != nil {
		return dData, err
	}
	for _, task := range tasks {
		for _, attachment := range task.NetworksAttachments {
			if !dd.permittedNetwork(attachment.Network.Spec.Name, dData.labeledNetwork) {
				continue
			}
			dData.networks = append(dData.networks, attachment.Network.Spec.Name)
			for _, address := range attachment.Addresses {
				parseCIDR(address, &dData.ipv4, &dData.ipv6)
			}
		}
	}
	dd.resolveHosts(&dData)
	log.Infof("[docker] Parsed service %s (%s). IPs: %v, Hosts: %v", dData.name, dData.id[:12], dData.ipv4, dData.hosts)

	return dData, nil
}

func serviceVirtualIPs(service *swarm.Service, ipv4, ipv6 *[]net.IP) {
	for _, vip := range service.Endpoint.VirtualIPs {
		parseCIDR(vip.Addr, ipv4, ipv6)
	}
}

func parseCIDR(address string, ipv4, ipv6 *[]net.IP) {
	addr, _, err := net.ParseCIDR(address)
	if addr == nil || err != nil {
		return
	}
	if addr.To4() != nil {
		if !ipsContain(*ipv4, addr) {
			*ipv4 = append(*ipv4, addr)
		}
	} else {
		if !ipsContain(*ipv6, addr) {
			*ipv6 = append(*ipv6, addr)
		}
	}
}

func ipsContain(ips []net.IP, ip net.IP) bool {
	for _, i := range ips {
		if i.Equal(ip) {
			return true
		}
	}
	return false
}

func (dd *DockerDiscovery) updateService(service *swarm.Service) error {
	c, err := dd.parseService(service)
	if err != nil {
		log.Infof("[docker] Parse service error %s: %s", c.name, err)
		if dd.hmap.ids.Has(c.id) {
			dd.hmap.removeContainer(c.id)
		}
		return err
	}
	if !dd.opts.enabledByDefault && !c.enabled && c.labeledHost == "" {
		log.Infof("[docker] Disabled service %s (%s)", c.name, c.id[:12])
		return nil
	}

	log.Infof("[docker] Add entry of service %s (%s). IP: %v. Hosts: %v",
		c.name, c.id[:12], c.ipv4, c.hosts)
	dd.hmap.addContainer(&c)
	return nil
}

func (dd *DockerDiscovery) removeService(serviceID string) error {
	ok := dd.hmap.ids.Has(serviceID)
	if !ok {
		return nil
	}
	dd.hmap.removeContainer(serviceID)
	return nil
}
