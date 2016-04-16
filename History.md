
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
