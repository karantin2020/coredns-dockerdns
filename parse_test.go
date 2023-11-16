package dockerdns

import (
	"reflect"
	"testing"

	"github.com/coredns/caddy"
)

func TestCreatePlugin(t *testing.T) {
	type args struct {
		c               *caddy.Controller
		envVars         [][2]string
		serverBlockKeys []string
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
					`docker {
					endpoint unix:///var/run/dockr.sock
					by_domain
					by_hostname
					by_label
					by_compose_domain
					networks dnsproxnet doknet
					ttl 3200
				}`),
				envVars: [][2]string{
					{dockerEnvEndpoint, "unix:///var/run/docker.sock"},
					{dockerEnvNetworks, "dnsproxynet,docknet"},
					{dockerEnvTTL, "2400"},
					{dockerEnvAutoEnable, "true"},
				},
				serverBlockKeys: []string{"loc."},
			},
			want: &DockerDiscovery{
				opts: dnsControlOpts{
					dockerEndpoint:   "unix:///var/run/docker.sock",
					byDomain:         true,
					byHostname:       true,
					byLabel:          true,
					byComposeDomain:  true,
					enabledByDefault: true,
					ttl:              2400,
					fromNetworks:     []string{"dnsproxynet", "docknet"},
				},
				Origins: []string{"loc."},
			},
			wantErr: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tt.args.c.ServerBlockKeys = tt.args.serverBlockKeys

			for _, j := range tt.args.envVars {
				t.Setenv(j[0], j[1])
			}

			got, err := createPlugin(tt.args.c)
			if (err != nil) != tt.wantErr {
				t.Errorf("createPlugin() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !reflect.DeepEqual(got.opts, tt.want.opts) {
				t.Errorf("createPlugin().opts = %v, want %v", got.opts, tt.want.opts)
			}
			if !reflect.DeepEqual(got.Origins, tt.want.Origins) {
				t.Errorf("createPlugin().Origins = %v, want %v", got.Origins, tt.want.Origins)
			}
			if got.opts.ttl != tt.want.opts.ttl {
				t.Errorf("createPlugin().ttl = %v, want %v", got.opts.ttl, tt.want.opts.ttl)
			}
			t.Logf("%+v", got)
		})
	}
}

func Test_validOriginArgs(t *testing.T) {
	type args struct {
		originArgs      []string
		serverBlockKeys []string
	}
	tests := []struct {
		name    string
		args    args
		wantErr bool
	}{
		{
			name: "Valid 1",
			args: args{
				originArgs:      []string{"dock."},
				serverBlockKeys: []string{"dns://.:53"},
			},
			wantErr: false,
		},
		{
			name: "Valid 2, no trailing dot",
			args: args{
				originArgs:      []string{"dock"},
				serverBlockKeys: []string{"dns://.:53"},
			},
			wantErr: false,
		},
		{
			name: "Valid 3, no trailing dot",
			args: args{
				originArgs:      []string{"rock s.rock"},
				serverBlockKeys: []string{"rock."},
			},
			wantErr: false,
		},
		{
			name: "Invalid 1",
			args: args{
				originArgs:      []string{"dock.", "s.dock."},
				serverBlockKeys: []string{"dns://rock.:53"},
			},
			wantErr: true,
		},
		{
			name: "Invalid 2, no trailing dot",
			args: args{
				originArgs:      []string{"dock", "s.dock"},
				serverBlockKeys: []string{"dns://rock.:53"},
			},
			wantErr: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := validOriginArgs(tt.args.originArgs, tt.args.serverBlockKeys)
			if (err != nil) != tt.wantErr {
				t.Errorf("validOriginArgs() error = %v, wantErr %v", err, tt.wantErr)
			}
			log.Infof("validOriginArgs got: %v", got)
		})
	}
}
