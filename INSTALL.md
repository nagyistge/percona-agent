Installation Guide
==================

- [Quick Install](#user-content-quick-install)
- [Standard Install](#user-content-standard-install)
- [Automated Install](#user-content-automated-install)
- [MySQL Auto-detection](#user-content-mysql-auto-detection)
- [MySQL Options](#user-content-mysql-options)
- [Non-MySQL Install](#user-content-non-mysql-install)
- [Updating the Agent](#user-content-updating-the-agent)
- [Uninstalling the Agent](#user-content-uninstalling-the-agent)

Quick Install
-------------

1. [Get your API key.](https://cloud.percona.com/api-key)
2. As root, run in the terminal:

`curl -s https://cloud.percona.com/install | bash /dev/stdin -api-key="<API key>"`

By default a quick install:
* Enables *Server Metrics Monitor*
* Enables *MySQL Metrics Monitor* if MySQL is present
* Enables *MySQL Query Analytics* if MySQL is present and running locally

A quick install requires [MySQL Auto-detection](#user-content-mysql-auto-detection) to work properly. If it fails (e.g. it can't connect to MySQL), only *Server Metrics Monitor* is enabled.

Standard Install
----------------

1. [Download the latest percona-agent.](http://www.percona.com/downloads/percona-agent/LATEST/)
2. Extract the tarball and change to the directory it creates.
3. Run the `install` script.

By default, the installer is automatic and interactive: it tries to do everything automatically as in a *Quick Install*, but if it has problems it prompts you for input. You can disable prompts by specifying `-interactive=false` for an [Automated Install](#user-content-automated-install).

Automated Install
-----------------

To automate installation of *percona-agent* (e.g. for Chef, Puppet, etc.) add the `-interacive=false` flag to a [Standard Install](#user-content-standard-install) to prevent the installer from prompting. A [Quick Install](#user-content-quick-install) can be used too because it sets `-interacive=false` by default.

Automation relies on:
* proper [MySQL Auto-detection](#user-content-mysql-auto-detection)
* or, specifying [MySQL Options](#user-content-mysql-options),
* or, a combination of both
If the installer fails to setup MySQL it will continue and enable only *Server Metrics Monitor*.

For example, without [MySQL Auto-detection](#user-content-mysql-auto-detection) you can specify the necesssary [MySQL Options](#user-content-mysql-options) instead:
```sh
./install -interactive=false -mysql-user=root -mysql-pass=secretpass -mysql-socket=/var/run/mysqld/mysqld.sock
```

**Note**: An automated install must create the percona-agent MySQL user; you cannot specify an existing MySQL user. This ability will be added in a future version of the installer.

MySQL Auto-detection
--------------------

The installer uses `mysql --print-defaults` to auto-detect the local MySQL instance and MySQL super user credentials. To ensure proper auto-detection, make sure `~/.my.cnf` (for root) has the necessary MySQL options to connect to MySQL as super user, e.g.:

```sh
user=root
password=pass
socket=/var/run/mysqld/mysqld.sock
```

MySQL super user credentials are used to create a MySQL user for the agent with these privileges:

* `SUPER, PROCESS, USAGE, SELECT ON *.* TO 'percona-agent'@HOST`
* `UPDATE, DELETE, DROP ON performance_schema.* TO 'percona-agent'@HOST`

`HOST` is `localhost` if a socket or `localhost` is used, else ``127.0.0.1` if that IP is used, else `%`.  Sometimes the privileges are granted to `localhost` and `127.0.0.1`.

The percona-agent MySQL user password is randomly generated and can be viewed later through the web app.

MySQL Options
-------------

| Flag              | Default | Description                 |
|-------------------|---------|-----------------------------|
|-mysql             | true    | Install for MySQL           |
|-create-mysql-user | true    | Create MySQL user for agent |
|-mysql-user        |         | MySQL username              |
|-mysql-pass        |         | MySQL password              |
|-mysql-host        |         | MySQL host                  |
|-mysql-port        |         | MySQL port                  |
|-mysql-socket      |         | MySQL socket file           |

To get list of all flags run `./install -help`

MySQL options specified on the command line override (take precedence over) MySQL options discovered by [MySQL Auto-detection](#user-content-mysql-auto-detection).

Slave Install
-------------

To install *percon-agent* on a slave, first install it on the master, then on the slave run the `install` script with `-create-mysql-user=false` and it will prompt you for the existing percona-agent MySQL user credentials.

Since this requires a prompt, a slave install does not currently work for an [Automated Install](#user-content-automated-installed).

Non-MySQL Install
-----------------

To install *percona-agent* on a server without MySQL (e.g. to monitor only server metrics), use `-mysql=false`:

```sh
./install -mysql=false
  ```
  
Updating the Agent
------------------

### With *Quick Install*

  When new version is available
  
  1. [Get your api-key](https://cloud.percona.com/api-key)
  2. Run in terminal as root:

`curl -s https://cloud.percona.com/install | bash /dev/stdin -api-key="<API key>"`

### With *Standard Install*

  1. [Download the latest percona-agent](http://www.percona.com/downloads/percona-agent/LATEST/) to your server.
  2. Extract the tarball.
  3. Run the `install` script.

Uninstalling the Agent
----------------------

First, to stop and remove *percona-agent* from a server, as root run either:

* `curl -s https://cloud.percona.com/install | /bin/sh /dev/stdin -uninstall` (if you did a [Quick Install](#user-content-quick-install))

or,

* `./install -uninstall` (if you did a [Standard Install](#user-content-standard-install))

Then [delete the agent](https://cloud.percona.com/agents) in the web app.  This removes its configuration and Query Analytics data from Percona Cloud Tools.

You can also [delete any MySQL instances](https://cloud.percona.com/instances/mysql) that the agent was monitoring.

Finally, you drop the percona-agent MySQL user from any MySQL instance the agent was monitoring by executing:

```sql
DROP USER 'percona-agent'@'localhost';
DROP USER 'percona-agent'@'127.0.0.1';
```
