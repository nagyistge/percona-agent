percona/percona-agent
=====================

This is percona-agent for [Percona Cloud Tools](https://cloud.percona.com).  It's a real-time client-side agent written in [Go](http://golang.org/) which implements the various servcies provided by Percona Cloud Tools (PCT).  You need a PCT account to install and use the agent.  [Sign up for free](https://cloud.percona.com/signup)!

One Line Install
-------------

1. [Get your api-key](https://cloud.percona.com/api-key)
2. Run in terminal:
   `curl -s https://cloud.percona.com/get | sudo bash /dev/stdin -api-key your-api-key-here`

More about *One Line Install* and other installation options can be found in our [Installation Guide](INSTALL.md)

Updating the Agent
------------------

Just use the same command as for `One Line Install` when new version is available, so:

1. [Get your api-key](https://cloud.percona.com/api-key)
2. Run in terminal:
   `curl -s https://cloud.percona.com/get | sudo bash /dev/stdin -api-key your-api-key-here`

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
