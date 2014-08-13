percona/percona-agent
=====================

- [About](#user-content-about)
- [Quick Install](#user-content-quick-install)
- [Updating the Agent](#user-content-updating-the-agent)
- [System Requirements](#user-content-system-requirements)
  - [MySQL monitor](#user-content-mysql-monitor)
  - [Server monitor](#user-content-server-monitor)
  - [Query Analytics for Slow Log](#user-content-query-analytics-for-slow-log)
  - [Query Analytics for Performance Schema](#user-content-query-analytics-for-performance-schema)
- [Supported Platforms and Versions](#user-content-supported-platforms-and-versions)
- [Help and Support](#user-content-help-and-support)

About
-----

This is percona-agent for [Percona Cloud Tools](https://cloud.percona.com).  It's a real-time client-side agent written in [Go](http://golang.org/) which implements the various servcies provided by Percona Cloud Tools (PCT).  You need a PCT account to install and use the agent.  [Sign up for free](https://cloud.percona.com/signup)!

Quick Install
-------------

1. [Get your API key.](https://cloud.percona.com/api-key)
2. Run in terminal as root:

`curl -s https://cloud.percona.com/install | bash /dev/stdin -api-key <API key>`

More about *Quick Install* and other installation options can be found in the [Installation Guide](INSTALL.md).

Updating the Agent
------------------

Just use the same command as for `Quick Install` when new version is available, so:

1. [Get your API key.](https://cloud.percona.com/api-key)
2. Run in terminal as root:

`curl -s https://cloud.percona.com/install | bash /dev/stdin -api-key <API key>`

System Requirements
-------------------

* Linux OS
* root access
* Outbound network access to `*.percona.com`, ports 80 and 443
* MySQL 5.1 or newer, any distro (Percona Server, MariaDB, etc.)

### MySQL monitor
* Local or remote access to MySQL
* MySQL user account with `PROCESS` and `USAGE` privileges

### Server monitor
* `/proc` filesystem
* Agent running on server

### Query Analytics for Slow Log
* Agent and MySQL running on the same server
* MySQL user account with `SUPER`, `USAGE`, and `SELECT` privileges

### Query Analytics for Performance Schema
* MySQL 5.6 or newer, any distro, including Amazon RDS
* MySQL user account with `SELECT`, `UPDATE`, `DELLTE` and `DROP` privileges on `performance_schema`

Supported Platforms and Versions
--------------------------------

* Any 32-bit (i386) or 64-bit (x86_64) Linux OS
* MySQL 5.1 through 5.6, any distro
* Amazon RDS (only MySQL monitor and Query Analytics for Performance Schema)

Help and Support
----------------

If you're a Percona Support customer, get help through the [Percona Customer Portal](https://customers.percona.com).

For bugs, please create an issue at https://jira.percona.com.

For everything else, please ask, share, and help others on the [Percona Cloud Tools Community Forum](http://www.percona.com/forums/questions-discussions/percona-cloud-tools).
