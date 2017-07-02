[![Go Report Card](https://goreportcard.com/badge/github.com/Luzifer/dockermanager)](https://goreportcard.com/report/github.com/Luzifer/dockermanager)
![](https://badges.fyi/github/license/Luzifer/dockermanager)
![](https://badges.fyi/github/downloads/Luzifer/dockermanager)
![](https://badges.fyi/github/latest-release/Luzifer/dockermanager)

# luzifer / dockermanager

The intention of this project is to have a running daemon on a [docker](https://www.docker.com/) host server which is able to realize a configuration of docker containers. For this it manages all containers and images on the docker host. This includes starting and stopping containers which are or are not defined by the configuration file.

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
  - `dockerproxy`: Configuration for the [dockerproxy](https://github.com/Luzifer/dockerproxy)
    - `slug`: Name part of the URL to map to this container
    - `port`: Published port (see `ports` above)
  - `add_cap`: Array of [capabilities](https://docs.docker.com/engine/reference/run/#runtime-privilege-and-linux-capabilities) to add to this container

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
    - MYVAR=value
  dockerproxy:
    slug: jenkins
    port: 1000
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

----

![](https://d2o84fseuhwkxk.cloudfront.net/dockermanager.svg)
