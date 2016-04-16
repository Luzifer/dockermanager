[![Go Report Card](https://goreportcard.com/badge/github.com/Luzifer/dockermanager)](https://goreportcard.com/report/github.com/Luzifer/dockermanager)
[![License: Apache v2.0](https://badge.luzifer.io/v1/badge?color=5d79b5&title=license&text=Apache+v2.0)](http://www.apache.org/licenses/LICENSE-2.0)
[![GoDoc reference](http://badge.luzifer.io/v1/badge?color=5d79b5&title=godoc&text=reference)](https://godoc.org/github.com/Luzifer/dockermanager/config)

# luzifer / dockermanager

The intention of this project is to have a running daemon on a [docker](https://www.docker.com/) host server which is able to realize a configuration of docker containers. For this it manages all containers and images on the docker host. This includes starting and stopping containers which are or are not defined by the configuration file.

## Missing / Planned Features

- Support load balancing / distribution of containers to create spread load on all servers
  - For this feature the serf master-election was built in. Currently it is not used.

## Requirements

- One or more host servers with latest [lxc-docker](https://docs.docker.com/installation/ubuntulinux/)
- A [serf](http://www.serfdom.io/)-agent running on each host and connected to one gossip-network
- A config file or config URL to serve the configuration from
- Docker daemon listening on tcp port
- The dockermanager set up
- If you want to use images from a private registry put a `.dockercfg` file (`docker login`) to the homedir of the user running dockermanager

## Configuration

### CLI parameters

```bash
# ./dockermanager --help
Usage of ./dockermanager:
  -c, --config="config.yaml": Config file or URL to read the config from
      --configInterval=10: Sleep time in minutes to wait between config reloads
      --docker-certs="": Directory containing cert.pem, key.pem, ca.pem for the registry
      --docker-host="tcp://192.168.59.103:2376": Connection method to the docker server
      --fullHost[=true]: Manage all containers on host
      --refreshInterval=30: fetch new images every <N> minutes
      --serfAddress="127.0.0.1:7373": Address of the serf agent to connect to
      --standalone[=false]: Do not use Serf to talk to other hosts
```

### Configuration file

The configuration is written in YAML format and reloaded regulary by the daemon:

- `container-name`: Name of the container on the host. Needs to be unique
  - `command`: Override CMD value set by Dockerfile
  - `hosts`: Array of hostnames (serf node names) to deploy the container to or `ALL`
  - `image`: Name of the image `registry` or `luzifer/jenkins` or `my.registry.com:5000/secret`
  - `tag`: Tag for the image, probably `latest`
  - `links`: Links to other containers in format `othercontainername:alias`
  - `volumes`: Volume mapping in form `<localdir>:<containerdir>`
  - `ports`: Array of port configurations
    - `container`: Exported port in the container e.g. `80/tcp` or `12201/udp`
    - `local`: IP/port combination in the form `<ip>:<port>`
  - `environment`: Array of enviroment variables in form `<key>=<value>`
  - `update_times`: Array of allowed time frames for updates of this container in format `HH:MM-HH:MM` (Optional, if not specified container is allowed to get updated all the time.)
  - `start_times`: Cron-style time specification when to start this container. Pay attention to choose a container quitting before your specified interval for this. Containers having this specification will not get started by default and are not restarted after they quit. Use this for starting cron-like tasks.
  - `stop_timeout`: Time in seconds to wait when stopping a deprecated container to be exchanged. (default: 5s)
  - `labels`: Labels to attach to the container (for example the config for the [DockerProxy](https://github.com/Luzifer/dockerproxy))

Example configuration for a jenkins container:

```yaml
---
jenkins:
  hosts:
    - docker01
  image: luzifer/jenkins
  tag: latest
  links:
    - "othercontainername:alias"
  volumes:
    - "/home/ubuntu/data/jenkins_home:/var/jenkins_home"
  ports:
    - container: 8080/tcp
      local: 0.0.0.0:1000
  environment:
    - ROUTER_SLUG=jenkins
    - ROUTER_PORT=1000
  update_times:
    - 04:00-06:00
  stop_timeout: 20


scheduletest:
  hosts:
    - docker01
  image: jlekie/curl
  tag: latest
  command:
    - "http://example.com/page"
  start_times: "*/2 * * * *"
```

## Development Status

Current version is marked as 0.x.x and for this it is to be considered unstable. As soon as the load balancing is implemented I will move to the 1.x.x version and the app will be stable within the major version.
