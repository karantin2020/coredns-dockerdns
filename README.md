## coredns-dockerdns  [![GoDoc][doc-img]][doc] [![Go Report Card][go-report-img]][go-report]
===================================

Docker discovery plugin for coredns

Name
----

dockerdns - add/remove A/AAAA DNS records for docker containers.

Syntax
------

    docker [ZONES...] {
        endpoint DOCKER_ENDPOINT
        by_domain
        by_hostname
        by_label
        by_compose_domain
        exposed_by_default
        ttl TTL
        from_networks NETWORKS...
    }

* `ZONES`: zones to apply for plugin (i.e.: loc, docker.local)
* `DOCKER_ENDPOINT`: the path to the docker socket. If unspecified, defaults to `unix:///var/run/docker.sock`. It can also be TCP socket, such as `tcp://127.0.0.1:999`.
* `by_domain`: expose container in dns by container name. Default is `false`
* `by_hostname`: expose container in dns by hostname. Default is `false`
* `by_label`: expose container in dns by label. Default is `true`, so it is of no use. This directive is always `true`
* `by_compose_domain`: expose container in dns by compose_domain. Default is `false`
* `exposed_by_default`: default is `false`
* `TTL`: change the DNS TTL (in seconds) of the records generated (forward and reverse). The default is 3600 seconds (1 hour).

#### Docker containers can have labels:
* `"coredns.dockernet.host"` - [string] specified container hostname 
* `"coredns.dockernet.network"` - [string] container network to apply. This parameter overwrites `from_networks` rule. BE CAUTIOUS!
* `"coredns.dockernet.enable"` - [true|false] enable specific container

#### Apply next host resolve rules:
* if `by_domain` == `true`:  
    `container_name.zone`
* if `by_hostname` == `true`:  
    `hostname.zone`
* if `by_label` == `true`:  
    `host` (from label value, must have the same zone as plugin)
* if `by_compose_domain` == `true`:  
    `service.project.zone`

It is recommended to define docker block in separate Corefile block

    # works incorrect 
    . {
        reload 10s
        hosts {
            172.28.0.4  whoami.gat
            172.28.0.4  whoami.nit
            fallthrough
        }
        docker docker.loc {
            by_domain
            by_hostname
            by_compose_domain
        }
        forward . 1.1.1.1 8.8.8.8
        errors
    }

    # works correct
    loc:15353 groc:15353 {
        reload 10s
        docker {
            by_domain
            by_hostname
            by_compose_domain
        }
        errors
    }

    # works correct too
    moc:15353 {
        reload 10s
        docker 
        errors
    }

How To Build
------------

    GO111MODULE=on go get -u github.com/coredns/coredns
    GO111MODULE=on go get github.com/karantin2020/coredns-dockerdns
    cd ~/go/src/github.com/coredns/coredns
    echo "docker:github.com/karantin2020/coredns-dockerdns" >> plugin.cfg
    cat plugin.cfg | uniq > plugin.cfg.tmp
    mv plugin.cfg.tmp plugin.cfg
    make all
    ~/go/src/github.com/coredns/coredns/coredns --version

Alternatively, you can use the following manual steps:

1. Checkout coredns:  `go get github.com/coredns/coredns`.
2. `cd $GOPATH/src/github.com/coredns/coredns`
3. `echo "docker:github.com/karantin2020/coredns-dockerdns" >> plugin.cfg`
4. `go generate`
5. `make`

Alternatively, run insider docker container

    docker build -t coredns-dockerdns .
    docker run --rm -v ${PWD}/Corefile:/etc/Corefile -v /var/run/docker.sock:/var/run/docker.sock -p 15353:15353/udp coredns-dockerdns -conf /etc/Corefile

Run tests

    go test -v

Example
-------

`Corefile`:

    loc:15353 groc:15353 {
        reload 10s
        docker {
            by_domain
            by_hostname
            by_compose_domain
        }
        errors
    }

    moc:15353 {
        reload 10s
        docker a.moc b.moc {
            by_domain
            by_hostname
            by_compose_domain
        }
        errors
    }

Start CoreDNS:

    $ ./coredns

    groc.:15353
    loc.:15353
    moc.:15353         
    CoreDNS-1.11.0

Start a docker container:

    $ docker run -d --name my-alpine --hostname alpine alpine sleep 1000
    78c2a06ef2a9b63df857b7985468f7310bba0d9ea4d0d2629343aff4fd171861

Use CoreDNS as your resolver to resolve the `my-alpine.loc` or `alpine.moc`, `alpine.groc`, `alpine.a.moc`, `alpine.b.moc`:

    $ dig @localhost -p 15353 my-alpine.loc

    ; <<>> DiG 9.10.3-P4-Ubuntu <<>> @localhost -p 15353 my-alpine.docker.loc
    ; (1 server found)
    ;; global options: +cmd
    ;; Got answer:
    ;; ->>HEADER<<- opcode: QUERY, status: NOERROR, id: 61786
    ;; flags: qr aa rd ra; QUERY: 1, ANSWER: 1, AUTHORITY: 0, ADDITIONAL: 1

    ;; OPT PSEUDOSECTION:
    ; EDNS: version: 0, flags:; udp: 4096
    ;; QUESTION SECTION:
    ;my-alpine.docker.loc.            IN      A

    ;; ANSWER SECTION:
    my-alpine.docker.loc.     3600    IN      A       172.17.0.2

    ;; Query time: 0 msec
    ;; SERVER: 127.0.0.1#15353(127.0.0.1)
    ;; WHEN: Thu Apr 26 22:39:55 EDT 2018
    ;; MSG SIZE  rcvd: 63

Stop the docker container will remove the corresponded DNS entries:

    $ docker stop my-alpine
    78c2a

    $ dig @localhost -p 15353 my-alpine.loc

    ;; QUESTION SECTION:
    ;my-alpine.docker.loc.            IN      A

Container will be resolved by label as ```nginx.loc```

    docker run --label=coredns.dockerdns.host=nginx.loc nginx


 See receipt [how install for local development](setup.md)


[doc-img]: https://godoc.org/github.com/karantin2020/coredns-dockerdns?status.svg
[doc]: https://godoc.org/github.com/karantin2020/coredns-dockerdns
[ci-img]: https://github.com/karantin2020/coredns-dockerdns/actions/workflows/build-test.yml/badge.svg
[ci]: https://github.com/karantin2020/coredns-dockerdns/actions/workflows/build-test.yml
[cov-img]: https://codecov.io/gh/karantin2020/coredns-dockerdns/branch/master/graph/badge.svg
[cov]: https://codecov.io/gh/karantin2020/coredns-dockerdns
[go-report-img]: https://goreportcard.com/badge/github.com/karantin2020/coredns-dockerdns
[go-report]: https://goreportcard.com/report/github.com/karantin2020/coredns-dockerdns