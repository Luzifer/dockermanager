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

## Configuration

The configuration is written in YAML format and reloaded regulary by the daemon:

- `container-name`: Name of the container on the host. Needs to be unique
  - `hosts`: Array of hostnames (serf node names) to deploy the container to or `ALL`
  - `image`: Name of the image `registry` or `luzifer/jenkins` or `my.registry.com:5000/secret`
  - `tag`: Tag for the image, probably `latest`
  - `volumes`: Volume mapping in form `<localdir>:<containerdir>`
  - `ports`: Array of port configurations
    - `container`: Exported port in the container e.g. `80/tcp` or `12201/udp`
    - `local`: IP/port combination in the form `<ip>:<port>`
  - `environment`: Array of enviroment variables in form `<key>=<value>`

Example configuration for a jenkins container:

```yaml
---
jenkins:
  hosts:
    - docker01
  image: luzifer/jenkins
  tag: latest
  volumes:
    - "/home/ubuntu/data/jenkins_home:/var/jenkins_home"
  ports:
    - container: 8080/tcp
      local: 0.0.0.0:1000
  environment:
    - ROUTER_SLUG=jenkins
    - ROUTER_PORT=1000
```
