package dockerdiscovery

import (
	"github.com/coredns/coredns/core/dnsserver"
	"github.com/coredns/coredns/plugin"
	dockerapi "github.com/fsouza/go-dockerclient"

	"github.com/coredns/caddy"
)

const (
	defaultDockerEndpoint = "unix:///var/run/docker.sock"
	defaultDockerDomain   = "loc"
	defaultTTL            = 3600
	dockerHostLabel       = "coredns.dockerdns.host"
	dockerNetworkLabel    = "coredns.dockerdns.network"
	dockerEnableLabel     = "coredns.dockerdns.enable"
	dockerProjectLabel    = "com.docker.compose.project"
	dockerServiceLabel    = "com.docker.compose.service"
)

func init() {
	caddy.RegisterPlugin("docker", caddy.Plugin{
		ServerType: "dns",
		Action:     setup,
	})
}

func createPlugin(c *caddy.Controller) (*DockerDiscovery, error) {
	var (
		dd  *DockerDiscovery
		err error
	)
	i := 0
	for c.Next() {
		if i > 0 {
			return nil, plugin.ErrOnce
		}
		i++

		dd, err = ParseStanza(c)
		if err != nil {
			return dd, err
		}
	}
	return dd, nil
}

func setup(c *caddy.Controller) error {
	dd, err := createPlugin(c)
	if err != nil {
		return err
	}

	err = dd.scanContainers()
	if err != nil {
		return err
	}

	stopChan := make(chan struct{})
	eventChan := make(chan *dockerapi.APIEvents)

	if err := dd.dockerClient.AddEventListener(eventChan); err != nil {
		log.Errorf("[docker] AddEventListener: %s", err)
		return err
	}

	go dd.start(stopChan, eventChan)

	c.OnShutdown(func() error {
		close(stopChan)
		close(eventChan)
		log.Info("[docker] Stop event listening")
		return nil
	})

	conf := dnsserver.GetConfig(c)
	conf.AddPlugin(func(next plugin.Handler) plugin.Handler {
		dd.Next = next
		return dd
	})
	return nil
}
