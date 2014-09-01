System Info
===========

About
-----

*System Info* provides nicely formatted information about *Server* and/or *MySQL* instance.

To access this information click on *System Info* link for chosen MySQL instance in [Metrics](https://cloud.percona.com/apps/metrics-monitor) or [Queries](https://cloud.percona.com/query-analytics/report) section of [cloud.percona.com](https://cloud.percona.com).
You can also access it from configuration section of [Server Instances](https://cloud.percona.com/instances/server) or [MySQL Instances](https://cloud.percona.com/instances/mysql)

.. image:: doc/images/system_info_metrics.png

And this is how the example report looks like

.. image:: doc/images/system_info_report.png

Requirements
------------

*System Info* requires:
* [Percona Agent](https://github.com/percona/percona-agent) in version 1.0.7 or greater
  Installation and update instructions for *Percona Agent* can be found [here](https://github.com/percona/percona-agent/blob/release/INSTALL.md)
* [Percona Toolkit](http://www.percona.com/software/percona-toolkit) installed on the same server as *Percona Agent*
  Installation instructions for *Percona Toolkit* can be found [here](http://www.percona.com/doc/percona-toolkit/2.2/installation.html)

Output
------

*System Info* report can be generated for *Server* or *MySQL* instance.
Each report has his own tab with a navigation bar on the left.
Navigation bar allows to quickly jump to different sections of the report.
Whole report and each section of it can be copied to clipboard (look for clipboard icon) and then pasted into emails without losing the formatting

** Server **
Runs a large variety of commands to inspect system status and its configuration. Report is split into sections:
* Server Info
* Processor
* Memory
* Mounted Filesystems
* Disk Schedulers And Queue Size
* Disk Partioning
* Kernel Inode State
* LVM Volumes
* LVM Volume Groups
* RAID Controller
* Network Config
* Interface Statistics
* Network Connections
* Top Processes
* Notable Processes
* Simplified and fuzzy rounded vmstat

Detailed information about each of those sections can be found in official [pt-summary documentation](http://www.percona.com/doc/percona-toolkit/2.2/pt-summary.html)

** MySQL **
Conveniently summarizes the status and configuration of a MySQL database server. Report is split into sections:
* MySQL Info
* Instances
* MySQL Executable
* Report On Port <port>
* Processlist
* Status Counters
* Table cache
* Key Percona Server features
* Percona XtraDB Cluster
* Plugins
* Query cache
* Schema
* Noteworthy Technologies
* InnoDB
* MyISAM
* Security
* Binary Logging
* Noteworthy Variables
* Configuration File

Detailed information about each of those sections can be found in official [pt-mysql-summary documentation](http://www.percona.com/doc/percona-toolkit/2.2/pt-mysql-summary.html)
