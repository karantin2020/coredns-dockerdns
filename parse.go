package dockerdiscovery

import (
	"errors"
	"fmt"
	"net"
	"regexp"
	"strconv"
	"strings"

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
	serverBlockKeys := c.ServerBlockKeys

	origins, err := validOriginArgs(originArgs, serverBlockKeys)
	if err != nil {
		log.Errorf("[docker] Invalid origin args: %s", err)
		return dd, err
	}

	dd.Origins = plugin.OriginsFromArgsOrServerBlock(origins, serverBlockKeys)

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
		case "fallthrough":
			dd.Fall.SetZonesFromArgs(c.RemainingArgs())
		case "endpoint":
			args := c.RemainingArgs()
			if len(args) > 0 {
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
		case "enabled_by_default":
			if c.NextArg() {
				return dd, c.ArgErr()
			}
			dd.opts.enabledByDefault = true
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
		case "no_reverse":
			if c.NextArg() {
				return dd, c.ArgErr()
			}
			dd.opts.autoReverse = false
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

// parseIP calls discards any v6 zone info, before calling net.ParseIP.
func parseIP(addr string) net.IP {
	if i := strings.Index(addr, "%"); i >= 0 {
		// discard ipv6 zone
		addr = addr[0:i]
	}

	return net.ParseIP(addr)
}

func validOriginArgs(originArgs, serverBlockKeys []string) ([]string, error) {
	if len(originArgs) == 0 {
		return originArgs, nil
	}
	serverBlock := make([]string, len(serverBlockKeys))
	copy(serverBlock, serverBlockKeys)
	for i := range serverBlock {
		serverBlock[i] = plugin.Host(serverBlock[i]).NormalizeExact()[0] // expansion of these already happened in dnsserver/register.go
	}

	origins := make([]string, 0, len(originArgs))
	for i := range originArgs {
		normalized := plugin.Name(originArgs[i]).Normalize()
		zone := plugin.Zones(serverBlock).Matches(normalized)
		if zone != "" {
			origins = append(origins, normalized)
		}
	}
	if len(originArgs) > 0 && len(origins) == 0 {
		return origins, fmt.Errorf("origin args of docker plugin: %v, and serverBlock Keys: %v, do not match",
			originArgs, serverBlock)
	}
	return origins, nil
}
