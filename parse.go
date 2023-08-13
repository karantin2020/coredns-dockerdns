package dockerdiscovery

import (
	"errors"
	"regexp"
	"strconv"

	clog "github.com/coredns/coredns/plugin/pkg/log"

	"github.com/coredns/caddy"

	"github.com/coredns/coredns/plugin"
	"github.com/coredns/coredns/plugin/pkg/dnsutil"
	dockerapi "github.com/fsouza/go-dockerclient"
)

var log = clog.NewWithPlugin("docker")

func ParseStanza(c *caddy.Controller) (*DockerDiscovery, error) {
	dd := NewDockerDiscovery("")

	originArgs := c.RemainingArgs()
	log.Debugf("[docker] Found origin args: %#v", originArgs)
	dd.Origins = plugin.OriginsFromArgsOrServerBlock(originArgs, c.ServerBlockKeys)

	primaryZoneIndex := -1
	for i, z := range dd.Origins {
		if dnsutil.IsReverse(z) > 0 {
			continue
		}
		primaryZoneIndex = i
		break
	}

	if primaryZoneIndex == -1 {
		return nil, errors.New("non-reverse zone name must be used")
	}

	for c.NextBlock() {
		log.Debug("[docker] ParseStanza next block")
		switch c.Val() {
		case "endpoint":
			args := c.RemainingArgs()
			if len(args) > 0 {
				if len(args) > 1 {
					log.Infof("[docker] DockerEndpoint is not provided, use first argument: %s",
						args[0])
				}
				dd.dockerEndpoint = args[0]
			} else {
				return dd, c.ArgErr()
			}
		case "by_domain":
			if c.NextArg() {
				return dd, c.ArgErr()
			}
			dd.opts.byDomain = true
		case "by_hostname":
			if c.NextArg() {
				return dd, c.ArgErr()
			}
			dd.opts.byHostname = true
		case "by_label":
			if c.NextArg() {
				return dd, c.ArgErr()
			}
			dd.opts.byLabel = true
		case "by_compose_domain":
			if c.NextArg() {
				return dd, c.ArgErr()
			}
			dd.opts.byComposeDomain = true
		case "exposed_by_default":
			if c.NextArg() {
				return dd, c.ArgErr()
			}
			dd.opts.exposedByDefault = true
		case "ttl":
			args := c.RemainingArgs()
			if len(args) == 0 {
				return nil, c.ArgErr()
			}
			t, err := strconv.Atoi(args[0])
			if err != nil {
				return nil, err
			}
			if t < 0 || t > 3600 {
				return nil, c.Errf("ttl must be in range [0, 3600]: %d", t)
			}
			dd.ttl = uint32(t)
		case "from_networks":
			networks := []string{}
			for c.NextArg() {
				name := c.Val()
				if validDockerNetworkName(name) {
					networks = append(networks, name)
				} else {
					log.Errorf("[docker] Invalid network name: %s", name)
				}
			}
			if len(networks) == 0 {
				return nil, c.ArgErr()
			}
			dd.opts.fromNetworks = networks
		default:
			return nil, c.Errf("Unknown directive '%s'", c.Val())
		}
	}
	dockerClient, err := dockerapi.NewClient(dd.dockerEndpoint)
	if err != nil || dockerClient == nil {
		log.Errorf("[docker] create docker client: %s", err)
		return dd, err
	}
	dd.dockerClient = dockerClient
	log.Debugf("[docker] dockerclient: %#v", dd.dockerClient)
	if len(dd.Origins) == 0 {
		log.Error("[docker] Error: no zones found")
		return nil, c.ArgErr()
	}
	dd.addRZones()

	return dd, nil
}

func (dd *DockerDiscovery) addRZones() {
	for i := range dd.Origins {
		dot := "."
		if dd.Origins[i] == "" || dd.Origins[i] == "." {
			dot = ""
		}
		dd.rzones = append(dd.rzones, dot+dd.Origins[i])
	}
}

// validate docker network name
func validDockerNetworkName(name string) bool {
	if name == "" {
		return false
	}
	regex := regexp.MustCompile("^[a-zA-Z0-9][a-zA-Z0-9_.-]+$")
	return regex.MatchString(name)
}
