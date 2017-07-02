[![Go Report Card](https://goreportcard.com/badge/github.com/Luzifer/dockermanager)](https://goreportcard.com/report/github.com/Luzifer/dockermanager)
![](https://badges.fyi/github/license/Luzifer/dockermanager)
![](https://badges.fyi/github/downloads/Luzifer/dockermanager)
![](https://badges.fyi/github/latest-release/Luzifer/dockermanager)

# luzifer / dockermanager

The intention of this project is to have a running daemon on a [docker](https://www.docker.com/) host server which is able to realize a configuration of docker containers. For this it manages all containers and images on the docker host. This includes starting and stopping containers which are or are not defined by the configuration file.

## Requirements

- One or more host servers with latest [docker-ce / docker-ee](https://store.docker.com/search?type=edition&offering=community)
- A config file or config URL to serve the configuration from
- Docker daemon listening on tcp port
- The dockermanager set up
- If you want to use images from a private registry put a `.dockercfg` file (`docker login`) to the homedir of the user running dockermanager

## Wasn't this supposed to be a cluster manager?

Yeah, speaking of pre-1.0 versions this is true. In the early development the dockermanager was intended to use a serf cluster to manage a whole cluster of machines. Because I never had the need to manage a cluster on my private projects I never really worked on the cluster logic.

In the meantime a bunch of cluster managers / schedulers having the capability to run Docker containers emerged and therefore I decided to cut out the cluster functionality. Following the principle "do one thing and do it well" the dockermanager now concentrates on managing single machines.

I'm managing a handfull of servers running as single nodes and that's where I'm optimizing the dockermanager. If you are searching for a solution to manage a cluster of machines you might want to take a look at these ones:

- [Amazon ECS](https://aws.amazon.com/ecs/) (Running Docker containers on a cluster of EC2s)
- [Hashicorp Nomad](https://www.nomadproject.io/) (Full cluster solution including ability to run Docker containers)
- [Kubernetes](https://kubernetes.io/) (Automated container deployment, scaling, and management)
- [Mesosphere DC/OS](https://mesosphere.com/product/) (OS built around containers and services)

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
```

### Configuration file

The configuration is written in YAML format and reloaded regularly by the daemon:

- `container-name`: Name of the container on the host. Needs to be unique
  - `command`: Override CMD value set by Dockerfile
  - `hosts`: Array of hostnames to deploy the container to or `ALL`
  - `image`: Name of the image `registry` or `luzifer/jenkins` or `my.registry.com:5000/secret`
  - `tag`: Tag for the image, probably `latest`
  - `links`: Links to other containers in format `othercontainername:alias`
  - `volumes`: Volume mapping in form `<localdir>:<containerdir>`
  - `ports`: Array of port configurations
    - `container`: Exported port in the container e.g. `80/tcp` or `12201/udp`
    - `local`: IP/port combination in the form `<ip>:<port>`
  - `environment`: Array of environment variables in form `<key>=<value>`
  - `update_times`: Array of allowed time frames for updates of this container in format `HH:MM-HH:MM` (Optional, if not specified container is allowed to get updated all the time.)
  - `start_times`: Cron-style time specification when to start this container. Pay attention to choose a container quitting before your specified interval for this. Containers having this specification will not get started by default and are not restarted after they quit. Use this for starting cron-like tasks.
  - `stop_timeout`: Time in seconds to wait when stopping a deprecated container to be exchanged. (default: 5s)
  - `labels`: Labels to attach to the container
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
