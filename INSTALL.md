Installation Guide
==================

- [Quick Install](#user-content-one-line-install)
- [Interactive Installation](#user-content-interactive-installation)
- [Automatic Non-Interactive Installation](#user-content-automatic-non-interactive-installation)
  - [With Quick Install](#user-content-with-one-line-install)
  - [With Regular Installer](#user-content-with-regular-installer)
    - [For installation on MySQL Master:](#user-content-for-installation-on-mysql-master)
    - [For installation on MySQL Slave:](#user-content-for-installation-on-mysql-slave)
    - [For installation on Non-MySQL server:](#user-content-for-installation-on-non-mysql-server)
- [Updating the Agent](#user-content-updating-the-agent)
  - [With Quick Install](#user-content-with-one-line-install-1)
  - [With Regular Installer](#user-content-with-regular-installer-1)
- [Uninstalling the Agent](#user-content-uninstalling-the-agent)
  - [With Quick Install](#user-content-with-one-line-install-2)
  - [With Regular Installer](#user-content-with-regular-installer-2)

Quick Install
----------------

1. [Get your api-key](https://cloud.percona.com/api-key)
2. Run in terminal:
   `curl -s https://cloud.percona.com/get | sudo bash /dev/stdin -api-key your-api-key-here`

By default installer:
* Enables *Server Metrics Monitor*
* Enables *MySQL Metrics Monitor* if possible
* Enables *MySQL Query Analytics* if possible

Installer uses `mysql --print-defaults` to detect MySQL instance and MySQL super user credentials.
So if you want to be sure that it is properly detected adjust your `~/.my.cnf` e.g.:

```sh
user=root
password=pass
socket=/var/run/mysqld/mysqld.sock
```

MySQL super user credentials are used for creating MySQL user which is specifically used by agent.
If you are interested, below are GRANT options with which it is created. **Don't add this user manually**, this is done by installer automatically.

```sql
GRANT SUPER, PROCESS, USAGE, SELECT ON *.* TO 'percona-agent'@'localhost' IDENTIFIED BY <random-password>
GRANT UPDATE, DELETE, DROP ON performance_schema.* TO 'percona-agent'@'localhost' IDENTIFIED BY <random-password>
```

Interactive Installation
------------------------

Instead of our *Quick Install* you can also use our *Regular Installer* for more traditional, interactive installation.

1. [Download the latest version of percona-agent](http://www.percona.com/downloads/percona-agent/LATEST/) to your server.
2. Extract the tarball.
3. Run the `install` script.

Automatic Non-Interactive Installation
--------------------------------------

To automate installation of *percona-agent* to multiple servers (e.g. installation with Chef/Puppet)
you can use either *Quick Install* or *Regular Installer*.

### With *Quick Install*

Use our *Quick Install* method, so:

1. [Get your api-key](https://cloud.percona.com/api-key)
2. Run in terminal:
   `curl -s https://cloud.percona.com/get | sudo bash /dev/stdin -api-key your-api-key-here`

For MySQL instance ensure `~/.my.cnf` is properly set before running *Quick Install*.
However, have in mind, that if installer fails to setup MySQL it will still continue and enable only Server Metrics Monitor

### With *Regular Installer*

Use our *Regular Installer* in non-interactive mode (`-non-interactive=true`) and pass all required parameters as flags, so:

1. [Download the latest version of percona-agent](http://www.percona.com/downloads/percona-agent/LATEST/) to your server.
2. Extract the tarball.
3. Run `install` script with flag `-non-interactive=true` and other flags you wish

Useful flags:

| Flag              | Default | Description                                                                                  |
|-------------------|---------|----------------------------------------------------------------------------------------------|
|-non-interactive   | false   | Non-interactive mode for headless installation                                               |
|-mysql             | true    | Install for MySQL                                                                            |
|-create-mysql-user | true    | Create MySQL user for agent, set it on Slave to false (used with -non-interactive=true mode) |
|-mysql-user        |         | MySQL username    (sets -non-interactive=true and -auto-detect-mysql=false)                  |
|-mysql-pass        |         | MySQL password    (sets -non-interactive=true and -auto-detect-mysql=false)                  |
|-mysql-host        |         | MySQL host        (sets -non-interactive=true and -auto-detect-mysql=false)                  |
|-mysql-port        |         | MySQL port        (sets -non-interactive=true and -auto-detect-mysql=false)                  |
|-mysql-socket      |         | MySQL socket file (sets -non-interactive=true and -auto-detect-mysql=false)                  |

To get list of all flags run `./install -help`

#### For installation on MySQL Master:

1. Provide credentials for super user with flags `-mysql-user`, `-mysql-pass`, `-mysql-host` or `-mysql-socket`.  

Example:
```sh
./install -non-interactive=true -mysql-user=root -mysql-pass=secretpass -mysql-socket=/var/run/mysqld/mysqld.sock
```

#### For installation on MySQL Slave:

1. Use `-create-mysql-user=false` flag
2. Provide credentials for existing `percona-agent` user replicated from master. Do this with flags `-mysql-user`, `-mysql-pass`, `-mysql-host` or `-mysql-socket`.
 Don't use super user credentials.

Example:
```sh
./install -non-interactive=true -create-mysql-user=false -mysql-user=percona-agent -mysql-pass=secretpass -mysql-socket=/var/run/mysqld/mysqld.sock
```

#### For installation on Non-MySQL server:

1. Use `-mysql=false` flag

Example:
```sh
./install -non-interactive=true -mysql=false
  ```
  
Updating the Agent
------------------

### With *Quick Install*

  When new version is available
  
  1. [Get your api-key](https://cloud.percona.com/api-key)
  2. Run in terminal `curl -s https://cloud.percona.com/get | sudo bash /dev/stdin -api-key your-api-key-here`

### With *Regular Installer*

  1. [Download the latest version of percona-agent](http://www.percona.com/downloads/percona-agent/LATEST/) to your server.
  2. Extract the tarball.
  3. Run the `install` script.

Uninstalling the Agent
----------------------

### With *Quick Install*

  1. Run in terminal `curl -s https://cloud.percona.com/get | sudo bash /dev/stdin -uninstall`

### With *Regular Installer*

  1. [Download the latest version of percona-agent](http://www.percona.com/downloads/percona-agent/LATEST/) to your server.
  2. Extract the tarball.
  3. Run in terminal `./install -uninstall`.
