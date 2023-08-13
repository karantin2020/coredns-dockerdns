package dockerdiscovery

import (
	"context"
	"fmt"

	"net"
	"strings"

	"github.com/coredns/coredns/plugin"
	"github.com/coredns/coredns/request"
	dockerapi "github.com/fsouza/go-dockerclient"
	csm "github.com/mhmtszr/concurrent-swiss-map"
	"github.com/miekg/dns"
)

// DockerDiscovery is a plugin that conforms to the coredns plugin interface
type DockerDiscovery struct {
	Next           plugin.Handler
	Origins        []string
	dockerEndpoint string
	dockerClient   *dockerapi.Client
	ttl            uint32
	opts           dnsControlOpts

	// mutex            sync.RWMutex
	// containerInfoMap ContainerInfoMap
	hmap   *Map
	rzones []string
}

type dnsControlOpts struct {
	byDomain         bool
	byHostname       bool
	byLabel          bool
	byComposeDomain  bool
	exposedByDefault bool
	fromNetworks     []string
}

// NewDockerDiscovery constructs a new DockerDiscovery object
func NewDockerDiscovery(dockerEndpoint string) *DockerDiscovery {
	if dockerEndpoint == "" {
		dockerEndpoint = defaultDockerEndpoint
	}
	return &DockerDiscovery{
		Origins:        make([]string, 0, 10),
		rzones:         make([]string, 0, 10),
		dockerEndpoint: dockerEndpoint,
		ttl:            defaultTTL,
		hmap: &Map{
			name4: newCSMap(),
			name6: newCSMap(),
			ids: csm.Create[string, *ContainerData](
				csm.WithShardCount[string, *ContainerData](32),
				csm.WithSize[string, *ContainerData](100),
			),
		},
		opts: dnsControlOpts{
			byLabel: true,
		},
	}
}

// ServeDNS implements plugin.Handler
func (dd *DockerDiscovery) ServeDNS(ctx context.Context, w dns.ResponseWriter, r *dns.Msg) (int, error) {
	state := request.Request{W: w, Req: r}
	qname := state.Name()
	log.Debugf("docker] Requested qname: %s", qname)
	zone := plugin.Zones(dd.Origins).Matches(qname)
	if zone == "" {
		// PTR zones don't need to be specified in Origins.
		if state.QType() != dns.TypePTR {
			// if this doesn't match we need to fall through regardless of h.Fallthrough
			return plugin.NextOrFailure(dd.Name(), dd.Next, ctx, w, r)
		}
	}
	var answers []dns.RR
	switch state.QType() {
	case dns.TypeA:
		ips, ok := dd.hmap.name4.Load(state.QName())
		if ok {
			log.Debugf("docker] Found ipv4 for qname: %s", qname)
			answers = a(qname, dd.ttl, ips)
		}
	case dns.TypeAAAA:
		ips, ok := dd.hmap.name6.Load(state.QName())
		if ok {
			log.Debugf("docker] Found ipv6 for qname: %s", qname)
			answers = aaaa(qname, dd.ttl, ips)
		}
	}

	if len(answers) == 0 {
		return dns.RcodeServerFailure, nil
	}

	m := new(dns.Msg)
	m.SetReply(r)
	m.Authoritative, m.RecursionAvailable, m.Compress = true, false, true
	m.Answer = answers

	state.SizeAndDo(m)
	m = state.Scrub(m)
	err := w.WriteMsg(m)
	if err != nil {
		log.Errorf("[docker]  %s", err.Error())
	}
	return dns.RcodeSuccess, nil
}

// Name implements plugin.Handler
func (dd *DockerDiscovery) Name() string {
	return "docker"
}

func (dd *DockerDiscovery) scanContainers() error {
	containers, err := dd.dockerClient.ListContainers(dockerapi.ListContainersOptions{})
	if err != nil {
		log.Errorf("[docker] ListContainers: %s", err)
		return err
	}

	for _, apiContainer := range containers {
		container, err := dd.dockerClient.InspectContainerWithOptions(dockerapi.InspectContainerOptions{ID: apiContainer.ID})
		if err != nil {
			log.Errorf("[docker] Inspect container %s: %s", container.ID[:12], err)
		}
		if err := dd.updateContainer(container); err != nil {
			log.Errorf("[docker] Adding A/AAAA records for container %s: %s", container.ID[:12], err)
		}
	}
	return nil
}

func (dd *DockerDiscovery) start(stopChan chan struct{}, events chan *dockerapi.APIEvents) {
	log.Info("[docker] Start event listening")
	for {
		select {
		case <-stopChan:
			return
		case msg := <-events:
			go func(msg *dockerapi.APIEvents) {
				event := fmt.Sprintf("%s:%s", msg.Type, msg.Action)
				switch event {
				case "container:start":

					container, err := dd.dockerClient.InspectContainerWithOptions(dockerapi.InspectContainerOptions{ID: msg.Actor.ID})
					if err != nil {
						log.Errorf("[docker] Event error %s #%s: %s", event, msg.Actor.ID[:12], err)
						return
					}
					log.Infof("[docker] New container %s spawned. Attempt to add A/AAAA records for it", container.ID[:12])
					if err := dd.updateContainer(container); err != nil {
						log.Errorf("[docker] Update container %s: %s", container.ID[:12], err)
					}
				case "container:die":
					log.Infof("[docker] Container %s stopped. Attempt to remove its A/AAAA records from the DNS", msg.Actor.ID[:12])
					if err := dd.removeContainer(msg.Actor.ID); err != nil {
						log.Errorf("[docker] Deleting A/AAAA records for container: %s: %s", msg.Actor.ID[:12], err)
					}
				case "network:connect":
					// take a look https://gist.github.com/josefkarasek/be9bac36921f7bc9a61df23451594fbf for example of same event's types attributes
					log.Infof("[docker] Container %s connected to network %s.", msg.Actor.Attributes["container"][:12], msg.Actor.Attributes["name"])

					container, err := dd.dockerClient.InspectContainerWithOptions(dockerapi.InspectContainerOptions{ID: msg.Actor.Attributes["container"]})
					if err != nil {
						log.Errorf("[docker] Event error %s #%s: %s", event, msg.Actor.Attributes["container"][:12], err)
						return
					}
					if err := dd.updateContainerNetworks(container); err != nil {
						log.Errorf("[docker] Update container %s: %s", container.ID[:12], err)
					}
				case "network:disconnect":
					log.Infof("[docker] Container %s disconnected from network %s", msg.Actor.Attributes["container"][:12], msg.Actor.Attributes["name"])

					container, err := dd.dockerClient.InspectContainerWithOptions(dockerapi.InspectContainerOptions{ID: msg.Actor.Attributes["container"]})
					if err != nil {
						log.Errorf("[docker] Event error %s #%s: %s", event, msg.Actor.Attributes["container"][:12], err)
						return
					}
					if err := dd.updateContainerNetworks(container); err != nil {
						log.Errorf("[docker] Update container %s: %s", container.ID[:12], err)
					}
				}
			}(msg)
		}
	}
}

// get ipv4 and ipv6 addresses for container.
func (dd *DockerDiscovery) getContainerAddresses(container *dockerapi.Container) (ipv4, ipv6 []net.IP, err error) {
	// save this away
	labeledNetwork, hasNetName := container.Config.Labels[dockerNetworkLabel]

	var networkMode string

	for {
		if container.NetworkSettings.IPAddress != "" && !hasNetName {
			ipv4i := net.ParseIP(container.NetworkSettings.IPAddress)
			if ipv4i != nil {
				log.Debugf("[docker] Container %s has IP %s", container.ID[:12], ipv4i)
				ipv4 = append(ipv4, ipv4i)
			}
		}

		if container.NetworkSettings.GlobalIPv6Address != "" && !hasNetName {
			ipv6i := net.ParseIP(container.NetworkSettings.GlobalIPv6Address)
			if ipv6i != nil {
				ipv6 = append(ipv6, ipv6i)
			}
		}

		networkMode = container.HostConfig.NetworkMode

		if networkMode == "host" {
			log.Infof("[docker] Container %s uses host network", container.ID[:12])
		}

		if strings.HasPrefix(networkMode, "container:") {
			log.Infof("Container %s is in another container's network namespace", container.ID[:12])
			otherID := container.HostConfig.NetworkMode[len("container:"):]
			container, err = dd.dockerClient.InspectContainerWithOptions(dockerapi.InspectContainerOptions{ID: otherID})
			if err != nil {
				return
			}
		} else {
			break
		}
	}

	var (
		network dockerapi.ContainerNetwork
		ok      = false
	)

	if hasNetName {
		log.Infof("[docker] Network name %s specified (%s)", labeledNetwork, container.ID[:12])
		network, ok = container.NetworkSettings.Networks[labeledNetwork]
		if ok {
			addressesFromNetwork(network, &ipv4, &ipv6)
		}
	} else {
		for netName, network := range container.NetworkSettings.Networks {
			if !dd.permittedNetwork(netName, labeledNetwork) {
				continue
			}
			log.Infof("[docker] Add network %s for container %s", netName, container.ID[:12])
			addressesFromNetwork(network, &ipv4, &ipv6)
			ok = true
		}
	}

	if !ok { // sometime while "network:disconnect" event fire
		err = fmt.Errorf("unable to find network settings for container %s", container.ID[:12])
		return
	}

	return
}

func addressesFromNetwork(network dockerapi.ContainerNetwork, ipv4 *[]net.IP, ipv6 *[]net.IP) {
	if len(network.IPAddress) > 0 {
		ipv4i := net.ParseIP(network.IPAddress) // ParseIP return nil when IPAddress equals ""
		if ipv4i != nil {
			log.Debugf("[docker] Container has IP %s", ipv4i)
			*ipv4 = append(*ipv4, ipv4i)
		}
	}
	if len(network.GlobalIPv6Address) > 0 {
		ipv6i := net.ParseIP(network.GlobalIPv6Address)
		if ipv6i != nil {
			*ipv6 = append(*ipv6, ipv6i)
		}
	}
	return
}

func (dd *DockerDiscovery) updateContainer(container *dockerapi.Container) error {
	c, err := dd.parseContainer(container)
	if err != nil {
		log.Debugf("[docker] Parsing container %s: %s", container.ID[:12], err)
		if dd.hmap.ids.Has(c.id) {
			log.Infof("[docker] Remove container entry %s (%s)",
				normalizeContainerName(container), container.ID[:12])
			dd.hmap.removeContainer(c.id)
		}
		return err
	}
	if !dd.opts.exposedByDefault && !c.enabled && c.labeledHost == "" {
		log.Infof("[docker] Skip container %s: disabled discovery", container.ID[:12])
		return nil
	}

	log.Infof("[docker] Add entry of container %s (%s). IP: %v. Hosts: %v",
		normalizeContainerName(container), container.ID[:12], c.ipv4, c.hosts)
	dd.hmap.addContainer(c)
	return nil
}

func (dd *DockerDiscovery) removeContainer(containerID string) error {
	cd, ok := dd.hmap.ids.Load(containerID)
	if !ok {
		log.Errorf("[docker] No entry associated with the container %s", containerID[:12])
		return nil
	}
	log.Infof("[docker] Delete entry %s (%s)", cd.name, cd.id[:12])
	dd.hmap.removeContainer(containerID)
	return nil
}

func (dd *DockerDiscovery) updateContainerNetworks(container *dockerapi.Container) error {
	c, ok := dd.hmap.ids.Load(container.ID)
	if !ok {
		log.Infof("[docker] No entry associated with the container %s", container.ID[:12])
		return nil
	}
	ipv4, ipv6, err := dd.getContainerAddresses(container)
	if err != nil {
		log.Errorf("[docker] Get container %s addresses: %s", c.id[:12], err)
		return err
	}
	if len(ipv4) == 0 {
		err = fmt.Errorf("no ipv4 address for container %s found", container.ID[:12])
		log.Errorf("[docker] Get container %s: %s", c.id[:12], err)
	}
	c.ipv4 = ipv4
	c.ipv6 = ipv6
	dd.hmap.addContainer(c)
	return nil
}
