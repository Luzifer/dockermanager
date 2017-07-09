# 1.2.0-rc5 / 2017-07-09

  * Fix: Maps need to be initialized

# 1.2.0-rc4 / 2017-07-09

  * Split locks into different lock topics

# 1.2.0-rc3 / 2017-07-09

  * Add deadlock detection during RC builds
  * Fix: Stating this once is enough
  * Do not take private fields in account for hashes

# 1.2.0-rc2 / 2017-07-09

  * Windows is no longer supported by runc
  * Fix naming of logrus package
  * Update all the dependencies 🙈

# 1.2.0-rc1 / 2017-07-09

  * Complete rewrite of the scheduler logic

# 1.1.0 / 2017-07-02

  * Change default docker-host to Unix socket
  * Fix: Some more linter errors

# 1.0.0 / 2017-07-02

- Clustering through serf is no longer supported which means the corresponding CLI flags are removed in this version. The `hosts` list is still supported and can be used to start different container constellations on different hosts from one configuration file.
- Native support for dockerproxy has been removed as dockerproxy is no longer supported and end-of-life. As an alternative plese take a look at [nginx-letsencrypt](https://github.com/Luzifer/nginx-letsencrypt) which is more flexible and supports for example web-sockets which is not possible in dockerproxy.

  * Breaking: Remove native support for dockerproxy
  * Breaking: Remove serf cluster logic
  * Improve logging
  * Fix: Remove several linter warnings

# 0.21.0 / 2017-07-02

  * Improve automated fetching of non-existent images

# 0.20.0 / 2016-12-15

  * Enable pushing assets to Github

# 0.19.4 / 2016-12-01

  * Fix: Do not modify labels of config when programmatically adding labels

# 0.19.3 / 2016-12-01

  * Move to structhash to prevent changing hash through map order

# 0.19.2 / 2016-10-08

  * Fix: Volumes behaves strange

# 0.19.1 / 2016-10-08

  * Fix: Differentiate between volume / bind mounts

# 0.19.0 / 2016-05-29

  * Remove containers managed by dockermanager and "Created"

# 0.18.1 / 2016-05-28

  * Fix: Do not delete images when not managing full host

# 0.18.0 / 2016-05-28

  * Force removal of "Dead" containers

# 0.17.1 / 2016-05-27

  * Fix: Docker config does not contain URLs but hostnames

# 0.17.0 / 2016-05-20

  * Added option to add capabilities

# 0.16.2 / 2016-05-17

  * Fix: Docker host-config deprication message

0.16.1 / 2016-04-16
==================

  * Fix: Type error

0.16.0 / 2016-04-16
==================

  * Added native support for dockerproxy
  * Refactored to export configuration
  * Fix: Ineffassign errors
  * Fix: Used `gofmt -s` on code

0.15.1 / 2015-11-08
==================

  * Fix: Images for "ALL" hosts should be present too

0.15.0 / 2015-11-07
==================

  * Do not keep images not required for current host

0.14.0 / 2015-09-10
==================

  * Added parameter for standalone operating

0.13.0 / 2015-08-29
==================

  * Added SIGHUP handling to reload the config
  * Updated third party libs

0.12.2 / 2015-08-02
==================

  * Fix: StopContainer is blocking, don't sleep after this
  * Updated README

0.12.1 / 2015-07-31
==================

  * Fix: Do not crash if no labels were specified

0.12.0 / 2015-07-31
==================

  * Added option to prevent managing full host

0.11.0 / 2015-07-31
==================

  * Support custom labels
  * Update containers on updated config

0.10.0 / 2015-07-30
==================

  * Made wait timeout on stop container configurable
  * Allow passing docker daemon certificates
  * Added CLI parameter to README

0.9.0 / 2015-07-17
==================

  * Added Godeps file
  * Improved CLI parameter parsing  
    **Attention:** This is a breaking change, you need to adjust your commandline parameters!
  * Fix: Lines with asterisk needs quoting

0.8.0 / 2015-07-16
==================

  * Added cron like task scheduling
  * Fix: On name collisions do a cleanup
  * Fix: Pull image when not present at container start

0.7.0 / 2015-05-24
==================

  * Remove not required images

0.6.0 / 2015-05-21
==================

  * Moved to more state-enforcer like setup
    * Breaks: -localInterval was removed and -refreshInterval introduced

0.5.0 / 2015-05-17
==================

  * Added docker registry authentication through .docercfg file

0.4.5 / 2015-04-28
==================

  * Fix: Reload list of running containers

0.4.4 / 2015-04-28
==================

  * Fixed log output for deprecated containers

0.4.3 / 2015-04-28
==================

  * Fix: API change did not longer return changed image ID

0.4.2 / 2015-03-21
==================

  * Fix: Only load new config if it could be loaded w/o error

0.4.1 / 2015-01-17
==================

  * Fix: Latest upstream version of library broke compatibility

0.4.0 / 2015-01-17
==================

  * Added update\_times feature

0.3.0 / 2014-12-14
==================

  * Added support for CMD override
  * Fix: Upstream API did change

0.2.0 / 2014-11-14
==================

  * Added support for container linking

0.1.0 / 2014-10-12
==================

  * First usable version
