percona/percona-agent
=====================

This is percona-agent for [Percona Cloud Tools](https://cloud.percona.com).  It's a real-time client-side agent written in [Go](http://golang.org/) which implements the various servcies provided by Percona Cloud Tools (PCT).  You need a PCT account to install and use the agent.  [Sign up for free](https://cloud.percona.com/signup)!

Quick Install
-------------

percona-agent must be installed _and_ ran as root.

1. [Download the latest version of percona-agent](http://www.percona.com/downloads/percona-agent/LATEST/) to your server.
1. Extract the tarball.
1. Run the `install` script.

Updating the Agent
------------------

Until we finsh implementing automatic self-update, updating the agent is a manual but quick and easy process.  As root:

1. [Download the latest version of percona-agent](http://www.percona.com/downloads/percona-agent/LATEST/) to your server and extract the tarball.
3. Run `service percona-agent stop`.  Make sure `percona-agent` is no longer running and that `/usr/local/percona/percona-agent.pid` was removed.  (There's currently a bug that sometimes causes the PID file not to be removed.)
4. Copy `bin/percona-agent` from the extracted tarball to `/usr/local/percona/percona-agent/bin`.
5. Run `/usr/local/percona/percona-agent/bin/percona-agent -version` to verify the new version.
6. Run `service percona-agent start`.

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

Upgrading from pt-agent
-----------------------

If you're already using Percona Cloud Tools by running pt-agent, the percona-agent installer will automatically detect, upgrade, and remove `pt-agent`.  `percona-agent` does everything `pt-agent` does and a lot more.

Help and Support
----------------

If you're a Percona Support customer, get help through the [Percona Customer Portal](https://customers.percona.com).

For bugs, please create an issue at https://jira.percona.com.

For everything else, please ask, share, and help others on the [Percona Cloud Tools Community Forum](http://www.percona.com/forums/questions-discussions/percona-cloud-tools).
