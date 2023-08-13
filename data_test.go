package dockerdiscovery

import (
	"encoding/json"
	"net"
	"os"
	"reflect"
	"testing"

	"github.com/coredns/caddy"
	dockerapi "github.com/fsouza/go-dockerclient"
)

func setupTestDD(t *testing.T) *DockerDiscovery {
	ctrlr := caddy.NewTestController("dns",
		`docker loc {
					endpoint unix:///var/run/docker.sock
					by_domain
					by_hostname
					by_label
					by_compose_domain
					exposed_by_default
					ttl 2400
				}`)
	dd, err := createPlugin(ctrlr)
	if err != nil {
		t.Fatalf("createPlugin() error = %v", err)
	}
	return dd
}

// unmarshal json from file inspect.test.json
func setupTestContainer(t *testing.T) *dockerapi.Container {
	fileName := "inspect.test.json"
	c := &dockerapi.Container{}
	data, err := os.ReadFile(fileName)
	if err != nil {
		t.Fatal(err)
	}
	err = json.Unmarshal(data, c)
	if err != nil {
		t.Fatal(err)
	}
	data, err = json.MarshalIndent(c, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	// log.Debugf("%s", string(data))
	return c
}

func TestParseContainer(t *testing.T) {
	c := setupTestContainer(t)
	type args struct {
		dd        *DockerDiscovery
		container *dockerapi.Container
	}
	tests := []struct {
		name    string
		args    args
		want    *ContainerData
		wantErr bool
	}{
		{
			name: "docker",
			args: args{
				dd:        setupTestDD(t),
				container: c,
			},
			want: &ContainerData{
				name:           c.Name[1:],
				id:             c.ID,
				hostname:       c.Config.Hostname,
				labeledHost:    c.Config.Labels[dockerHostLabel],
				labeledNetwork: c.Config.Labels[dockerNetworkLabel],
				enabled:        c.Config.Labels[dockerEnableLabel] == "true",
				project:        c.Config.Labels[dockerProjectLabel],
				service:        c.Config.Labels[dockerServiceLabel],
				networks:       []string{"dnsproxynet"},
				ipv4:           []net.IP{net.ParseIP("172.28.0.4")},
				ipv6:           nil,
				hosts: []string{
					"whoami.loc.",
					"whoami.dns-proxy.loc.",
					"w.loc.",
				},
			},
			wantErr: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dd := tt.args.dd
			got, err := dd.parseContainer(tt.args.container)
			if (err != nil) != tt.wantErr {
				t.Errorf("DockerDiscovery.parseContainer() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("DockerDiscovery.parseContainer() = %#v, want %#v", got, tt.want)
			}
			log.Debugf("[docker] test got: %+v", got)
			log.Debugf("[docker] test tt.want: %+v", tt.want)
		})
	}
}
