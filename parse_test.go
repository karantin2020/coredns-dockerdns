package dockerdiscovery

import (
	"reflect"
	"testing"

	"github.com/coredns/caddy"
)

func TestCreatePlugin(t *testing.T) {
	type args struct {
		c *caddy.Controller
	}
	tests := []struct {
		name    string
		args    args
		want    *DockerDiscovery
		wantErr bool
	}{
		{
			name: "docker",
			args: args{
				c: caddy.NewTestController("dns",
					`docker loc {
					endpoint unix:///var/run/docker.sock
					by_domain
					by_hostname
					by_label
					by_compose_domain
					exposed_by_default
					ttl 2400
				}`),
			},
			want: &DockerDiscovery{
				opts: dnsControlOpts{
					byDomain:         true,
					byHostname:       true,
					byLabel:          true,
					byComposeDomain:  true,
					exposedByDefault: true,
				},
				ttl:     2400,
				Origins: []string{"loc."},
			},
			wantErr: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := createPlugin(tt.args.c)
			if (err != nil) != tt.wantErr {
				t.Errorf("createPlugin() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !reflect.DeepEqual(got.opts, tt.want.opts) {
				t.Errorf("createPlugin().opts = %v, want %v", got, tt.want)
			}
			if !reflect.DeepEqual(got.Origins, tt.want.Origins) {
				t.Errorf("createPlugin().Origins = %v, want %v", got.Origins, tt.want.Origins)
			}
			if got.ttl != tt.want.ttl {
				t.Errorf("createPlugin().ttl = %v, want %v", got.ttl, tt.want.ttl)
			}
			log.Debugf("%+v", got)
		})
	}
}
