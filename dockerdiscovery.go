package dockerdns

import (
	"context"
	"fmt"

	"net"
	"strings"

	"github.com/coredns/coredns/plugin"
	"github.com/coredns/coredns/plugin/pkg/dnsutil"
	"github.com/coredns/coredns/plugin/pkg/fall"
	"github.com/coredns/coredns/request"
	dockerapi "github.com/fsouza/go-dockerclient"
	csm "github.com/mhmtszr/concurrent-swiss-map"
	"github.com/miekg/dns"
)

// DockerDiscovery is a plugin that conforms to the coredns plugin interface
type DockerDiscovery struct {
	Next         plugin.Handler
	Origins      []string
	dockerClient *dockerapi.Client
	Fall         fall.F
	opts         dnsControlOpts

	// mutex            sync.RWMutex
	// containerInfoMap ContainerInfoMap
	hmap   *Map
	rzones []string
}

type dnsControlOpts struct {
	dockerEndpoint   string
	byDomain         bool
	byHostname       bool
	byLabel          bool
	byComposeDomain  bool
	enabledByDefault bool
	fromNetworks     []string
	ttl              uint32
	autoReverse      bool
}

// NewDockerDiscovery constructs a new DockerDiscovery object
func NewDockerDiscovery(dockerEndpoint string) *DockerDiscovery {
	if dockerEndpoint == "" {
		dockerEndpoint = defaultDockerEndpoint
	}
	dd := &DockerDiscovery{
		Origins: make([]string, 0, 10),
		rzones:  make([]string, 0, 10),
		hmap: &Map{
			name4: newCSMap(),
			name6: newCSMap(),
			ids: csm.Create[string, *ContainerData](
				csm.WithShardCount[string, *ContainerData](32),
				csm.WithSize[string, *ContainerData](100),
			),
			addr: csm.Create[string, []string](
				csm.WithShardCount[string, []string](32),
				csm.WithSize[string, []string](100),
			),
		},
		opts: dnsControlOpts{
			dockerEndpoint: dockerEndpoint,
			byLabel:        true,
			ttl:            defaultTTL,
		},
	}
	dd.hmap.autoReverse = &dd.opts.autoReverse
	return dd
}

// ServeDNS implements plugin.Handler
func (dd *DockerDiscovery) ServeDNS(ctx context.Context, w dns.ResponseWriter, r *dns.Msg) (int, error) {
	state := request.Request{W: w, Req: r}
	qname := state.Name()
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
	case dns.TypePTR:
		addr := dnsutil.ExtractAddressFromReverse(qname)
		names, ok := dd.hmap.addr.Load(addr)
		if !ok {
			return plugin.NextOrFailure(dd.Name(), dd.Next, ctx, w, r)
		}
		answers = ptr(qname, dd.opts.ttl, names)
	case dns.TypeA:
		ips, ok := dd.hmap.name4.Load(state.QName())
		if ok {
			answers = a(qname, dd.opts.ttl, ips)
		}
	case dns.TypeAAAA:
		ips, ok := dd.hmap.name6.Load(state.QName())
		if ok {
			answers = aaaa(qname, dd.opts.ttl, ips)
		}
	}

	// Only on NXDOMAIN we will fallthrough.
	if len(answers) == 0 {
		if dd.Fall.Through(qname) {
			return plugin.NextOrFailure(dd.Name(), dd.Next, ctx, w, r)
		}

		// We want to send an NXDOMAIN, but because of /etc/hosts' setup we don't have a SOA, so we make it SERVFAIL
		// to at least give an answer back to signals we're having problems resolving this.
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
		log.Errorf("error write message: %s", err)
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
			continue
		}
		dd.updateContainer(container)
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
					if err := dd.updateContainer(container); err != nil {
						log.Errorf("[docker] update container %s: %s", container.ID[:12], err)
					}
				case "container:die":
					if err := dd.removeContainer(msg.Actor.ID); err != nil {
						log.Errorf("[docker] Deleting A/AAAA records for container: %s: %s", msg.Actor.ID[:12], err)
					}
				case "network:connect":
					// take a look https://gist.github.com/josefkarasek/be9bac36921f7bc9a61df23451594fbf for example of same event's types attributes
					container, err := dd.dockerClient.InspectContainerWithOptions(dockerapi.InspectContainerOptions{ID: msg.Actor.Attributes["container"]})
					if err != nil {
						log.Errorf("[docker] Event error %s #%s: %s", event, msg.Actor.Attributes["container"][:12], err)
						return
					}
					if err := dd.updateContainerNetworks(container); err != nil {
						log.Errorf("[docker] update container %s: %s", container.ID[:12], err)
					}
				case "network:disconnect":
					container, err := dd.dockerClient.InspectContainerWithOptions(dockerapi.InspectContainerOptions{ID: msg.Actor.Attributes["container"]})
					if err != nil {
						log.Errorf("[docker] Event error %s #%s: %s", event, msg.Actor.Attributes["container"][:12], err)
						return
					}
					if err := dd.updateContainerNetworks(container); err != nil {
						log.Errorf("[docker] update container %s: %s", container.ID[:12], err)
					}
				}
			}(msg)
		}
	}
}

// get ipv4 and ipv6 addresses for container.
func (dd *DockerDiscovery) getContainerAddresses(container *dockerapi.Container) (ipv4, ipv6 []net.IP, err error) {

	var networkMode string

	for {
		if container.NetworkSettings.IPAddress != "" {
			ipv4i := parseIP(container.NetworkSettings.IPAddress)
			if ipv4i != nil {
				ipv4 = append(ipv4, ipv4i)
			}
		}

		if container.NetworkSettings.GlobalIPv6Address != "" {
			ipv6i := parseIP(container.NetworkSettings.GlobalIPv6Address)
			if ipv6i != nil {
				ipv6 = append(ipv6, ipv6i)
			}
		}

		networkMode = container.HostConfig.NetworkMode

		// if networkMode == "host" {
		// 	log.Infof("[docker] Container %s uses host network", container.ID[:12])
		// }

		if strings.HasPrefix(networkMode, "container:") {
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
		netName string
		ok      = false
	)

	for netName, network = range container.NetworkSettings.Networks {
		if !dd.permittedNetwork(netName) {
			continue
		}
		addressesFromNetwork(network, &ipv4, &ipv6)
		ok = true
	}

	if !ok { // sometime while "network:disconnect" event fire
		err = fmt.Errorf("[docker] no permitted networks of container %s: %s", container.ID[:12], normalizeContainerName(container))
	}

	return
}

func addressesFromNetwork(network dockerapi.ContainerNetwork, ipv4, ipv6 *[]net.IP) {
	if len(network.IPAddress) > 0 {
		ipv4i := parseIP(network.IPAddress) // ParseIP return nil when IPAddress equals ""
		if ipv4i != nil {
			*ipv4 = append(*ipv4, ipv4i)
		}
	}
	if len(network.GlobalIPv6Address) > 0 {
		ipv6i := parseIP(network.GlobalIPv6Address)
		if ipv6i != nil {
			*ipv6 = append(*ipv6, ipv6i)
		}
	}
}

func (dd *DockerDiscovery) updateContainer(container *dockerapi.Container) error {
	c, err := dd.parseContainer(container)
	if err != nil || c.forceDisabled || (!dd.opts.enabledByDefault && !c.enabled) {
		if dd.hmap.ids.Has(c.id) {
			dd.hmap.removeContainer(c.id)
		}
		return err
	}

	log.Infof("[docker] add entry of container %s (%s). IP: %v. Hosts: %v",
		normalizeContainerName(container), container.ID[:12], c.ipv4, c.hosts)
	dd.hmap.addContainer(c)
	return nil
}

func (dd *DockerDiscovery) removeContainer(containerID string) error {
	ok := dd.hmap.ids.Has(containerID)
	if !ok {
		return nil
	}
	dd.hmap.removeContainer(containerID)
	return nil
}

func (dd *DockerDiscovery) updateContainerNetworks(container *dockerapi.Container) error {
	c, ok := dd.hmap.ids.Load(container.ID)
	if !ok {
		return nil
	}
	ipv4, ipv6, err := dd.getContainerAddresses(container)
	if err != nil {
		return err
	}
	c.ipv4 = ipv4
	c.ipv6 = ipv6
	dd.hmap.addContainer(c)
	return nil
}
