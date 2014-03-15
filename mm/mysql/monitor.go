/*
   Copyright (c) 2014, Percona LLC and/or its affiliates. All rights reserved.

   This program is free software: you can redistribute it and/or modify
   it under the terms of the GNU Affero General Public License as published by
   the Free Software Foundation, either version 3 of the License, or
   (at your option) any later version.

   This program is distributed in the hope that it will be useful,
   but WITHOUT ANY WARRANTY; without even the implied warranty of
   MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
   GNU Affero General Public License for more details.

   You should have received a copy of the GNU Affero General Public License
   along with this program.  If not, see <http://www.gnu.org/licenses/>
*/

package mysql

import (
	"database/sql"
	"fmt"
	_ "github.com/go-sql-driver/mysql"
	"github.com/percona/cloud-tools/instance"
	"github.com/percona/cloud-tools/mm"
	"github.com/percona/cloud-tools/mysql"
	"github.com/percona/cloud-tools/pct"
	"strconv"
	"strings"
	"time"
)

type Monitor struct {
	name   string
	config *Config
	logger *pct.Logger
	conn   mysql.Connector
	// --
	tickChan       chan time.Time
	collectionChan chan *mm.Collection
	connected      bool
	connectedChan  chan bool
	status         *pct.Status
	sync           *pct.SyncChan
	running        bool
}

func NewMonitor(name string, config *Config, logger *pct.Logger, conn mysql.Connector) *Monitor {

	m := &Monitor{
		name:   name,
		logger: logger,
		conn:   conn,
		// --
		connectedChan: make(chan bool, 1),
		status:        pct.NewStatus([]string{name, name + "-mysql"}),
		sync:          pct.NewSyncChan(),
	}
	return m
}

/////////////////////////////////////////////////////////////////////////////
// Interface
/////////////////////////////////////////////////////////////////////////////

// @goroutine[0]
func (m *Monitor) Start(tickChan chan time.Time, collectionChan chan *mm.Collection) error {
	if m.running {
		return pct.ServiceIsRunningError{m.name}
	}

	m.tickChan = tickChan
	m.collectionChan = collectionChan

	go m.run()
	m.running = true

	return nil
}

// @goroutine[0]
func (m *Monitor) Stop() error {
	if !m.running {
		return nil // already stopped
	}

	// Stop run().  When it returns, it updates status to "Stopped".
	m.status.Update(m.name, "Stopping")
	m.sync.Stop()
	m.sync.Wait()

	m.config = nil // no config if not running

	// Do not update status to "Stopped" here; run() does that on return.
	return nil
}

// @goroutine[0]
func (m *Monitor) Status() map[string]string {
	return m.status.All()
}

// @goroutine[0]
func (m *Monitor) TickChan() chan time.Time {
	return m.tickChan
}

// @goroutine[0]
func (m *Monitor) Config() interface{} {
	return m.config
}

/////////////////////////////////////////////////////////////////////////////
// Implementation
/////////////////////////////////////////////////////////////////////////////

// run:@goroutine[3]
func (m *Monitor) connect(err error) {
	// Close/release previous connection, if any.
	m.conn.Close()

	// Try forever to connect to MySQL...
	for {
		if err != nil {
			m.status.Update(m.name+"-mysql", fmt.Sprintf("Connecting (%s)", err))
		} else {
			m.status.Update(m.name+"-mysql", fmt.Sprintf("Connecting"))
		}
		if err = m.conn.Connect(1); err != nil {
			m.logger.Warn(err)
			continue
		}

		// Set global vars we need.  If these fail, that's ok: they won't work,
		// but don't let that stop us from collecting other metrics.
		if m.config.InnoDB != "" {
			sql := `SET GLOBAL innodb_monitor_enable = "` + m.config.InnoDB + `"`
			if _, err := m.conn.DB().Exec(sql); err != nil {
				m.logger.Error(sql, err)
			}
		}

		if m.config.UserStats {
			// 5.1.49 <= v <= 5.5.10: SET GLOBAL userstat_running=ON
			// 5.5.10 <  v:           SET GLOBAL userstat=ON
			sql := "SET GLOBAL userstat=ON"
			if _, err := m.conn.DB().Exec(sql); err != nil {
				m.logger.Error(sql, err)
			}
		}

		// Tell run() goroutine that it can try to collect metrics.
		// If connection is lost, it will call us again.
		m.status.Update(m.name+"-mysql", "Connected")
		m.connectedChan <- true
	}
}

// @goroutine[2]
func (m *Monitor) run() {
	go m.connect(nil)
	defer func() {
		m.conn.Close()
		m.status.Update(m.name, "Stopped")
		m.sync.Done()
	}()

	for {
		select {
		case now := <-m.tickChan:
			if !m.connected {
				continue
			}

			m.status.Update(m.name, "Running")

			c := &mm.Collection{
				Config: instance.Config{
					Service:    m.config.Service,
					InstanceId: m.config.InstanceId,
				},
				Ts:      now.UTC().Unix(),
				Metrics: []mm.Metric{},
			}

			// Get collection of metrics.
			conn := m.conn.DB()
			m.GetShowStatusMetrics(conn, c)
			if m.config.InnoDB != "" {
				m.GetInnoDBMetrics(conn, c)
			}
			if m.config.UserStats {
				m.getTableUserStats(conn, c, m.config.UserStatsIgnoreDb)
				m.getIndexUserStats(conn, c, m.config.UserStatsIgnoreDb)
			}

			// Send the metrics (to an mm.Aggregator).
			if len(c.Metrics) > 0 {
				select {
				case m.collectionChan <- c:
				case <-time.After(500 * time.Millisecond):
					// lost collection
					m.logger.Debug("Lost MySQL metrics; timeout spooling after 500ms")
				}
			} else {
				m.logger.Debug("No metrics") // shouldn't happen
			}

			m.status.Update(m.name, "Ready")
		case connected := <-m.connectedChan:
			m.connected = connected
			if connected {
				m.status.Update(m.name, "Ready")
				m.logger.Debug("Connected")
			} else {
				m.logger.Debug("Disconnected")
				go m.connect(nil)
			}
		case <-m.sync.StopChan:
			return
		}
	}
}

// --------------------------------------------------------------------------
// SHOW STATUS
// --------------------------------------------------------------------------

// @goroutine[2]
func (m *Monitor) GetShowStatusMetrics(conn *sql.DB, c *mm.Collection) error {
	rows, err := conn.Query("SHOW /*!50002 GLOBAL */ STATUS")
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var statName string
		var statValue string
		if err = rows.Scan(&statName, &statValue); err != nil {
			return err
		}

		statName = strings.ToLower(statName)
		metricType, ok := m.config.Status[statName]
		if !ok {
			continue // not collecting this stat
		}

		metricName := statName
		metricValue, err := strconv.ParseFloat(statValue, 64)
		if err != nil {
			metricValue = 0.0
		}

		c.Metrics = append(c.Metrics, mm.Metric{metricName, metricType, metricValue, ""})
	}
	err = rows.Err()
	if err != nil {
		return err
	}
	return nil
}

// --------------------------------------------------------------------------
// InnoDB Metrics
// http://dev.mysql.com/doc/refman/5.6/en/innodb-metrics-table.html
// https://blogs.oracle.com/mysqlinnodb/entry/get_started_with_innodb_metrics
// --------------------------------------------------------------------------

// @goroutine[2]
func (m *Monitor) GetInnoDBMetrics(conn *sql.DB, c *mm.Collection) error {
	rows, err := conn.Query("SELECT NAME, SUBSYSTEM, COUNT, TYPE FROM INFORMATION_SCHEMA.INNODB_METRICS WHERE STATUS='enabled'")
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var statName string
		var statSubsystem string
		var statCount string
		var statType string
		err = rows.Scan(&statName, &statSubsystem, &statCount, &statType)
		if err != nil {
			return err
		}

		metricName := "innodb/" + strings.ToLower(statSubsystem) + "/" + strings.ToLower(statName)
		metricValue, err := strconv.ParseFloat(statCount, 64)
		if err != nil {
			metricValue = 0.0
		}
		var metricType string
		if statType == "value" {
			metricType = "gauge"
		} else {
			metricType = "counter"
		}
		c.Metrics = append(c.Metrics, mm.Metric{metricName, metricType, metricValue, ""})
	}
	err = rows.Err()
	if err != nil {
		return err
	}
	return nil
}

// --------------------------------------------------------------------------
// User Statistics
// http://www.percona.com/doc/percona-server/5.5/diagnostics/user_stats.html
// --------------------------------------------------------------------------

// @goroutine[2]
func (m *Monitor) getTableUserStats(conn *sql.DB, c *mm.Collection, ignoreDb string) error {
	/**
	 *  SELECT * FROM INFORMATION_SCHEMA.TABLE_STATISTICS;
	 *  +--------------+-------------+-----------+--------------+------------------------+
	 *  | TABLE_SCHEMA | TABLE_NAME  | ROWS_READ | ROWS_CHANGED | ROWS_CHANGED_X_INDEXES |
	 */
	sql := "SELECT TABLE_SCHEMA, TABLE_NAME, ROWS_READ, ROWS_CHANGED, ROWS_CHANGED_X_INDEXES" +
		" FROM INFORMATION_SCHEMA.TABLE_STATISTICS"
	if ignoreDb != "" {
		sql += " WHERE TABLE_SCHEMA NOT LIKE '" + ignoreDb + "'"
	}
	rows, err := conn.Query(sql)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var tableSchema string
		var tableName string
		var rowsRead int64
		var rowsChanged int64
		var rowsChangedIndexes int64
		err = rows.Scan(&tableSchema, &tableName, &rowsRead, &rowsChanged, &rowsChangedIndexes)
		if err != nil {
			return err
		}

		c.Metrics = append(c.Metrics, mm.Metric{
			Name:   "db." + tableSchema + "/t." + tableName + "/rows_read",
			Type:   "counter",
			Number: float64(rowsRead),
		})
		c.Metrics = append(c.Metrics, mm.Metric{
			Name:   "db." + tableSchema + "/t." + tableName + "/rows_changed",
			Type:   "counter",
			Number: float64(rowsChanged),
		})
		c.Metrics = append(c.Metrics, mm.Metric{
			Name:   "db." + tableSchema + "/t." + tableName + "/rows_changed_x_indexes",
			Type:   "counter",
			Number: float64(rowsChangedIndexes),
		})
	}
	err = rows.Err()
	if err != nil {
		return err
	}
	return nil
}

// @goroutine[2]
func (m *Monitor) getIndexUserStats(conn *sql.DB, c *mm.Collection, ignoreDb string) error {
	/**
	 *  SELECT * FROM INFORMATION_SCHEMA.INDEX_STATISTICS;
	 *  +--------------+-------------+------------+-----------+
	 *  | TABLE_SCHEMA | TABLE_NAME  | INDEX_NAME | ROWS_READ |
	 *  +--------------+-------------+------------+-----------+
	 */
	sql := "SELECT TABLE_SCHEMA, TABLE_NAME, INDEX_NAME, ROWS_READ" +
		" FROM INFORMATION_SCHEMA.INDEX_STATISTICS"
	if ignoreDb != "" {
		sql = sql + " WHERE TABLE_SCHEMA NOT LIKE '" + ignoreDb + "'"
	}
	rows, err := conn.Query(sql)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var tableSchema string
		var tableName string
		var indexName string
		var rowsRead int64
		err = rows.Scan(&tableSchema, &tableName, &indexName, &rowsRead)
		if err != nil {
			return err
		}

		metricName := "db." + tableSchema + "/t." + tableName + "/idx." + indexName + "/rows_read"
		metricValue := float64(rowsRead)
		c.Metrics = append(c.Metrics, mm.Metric{metricName, "counter", metricValue, ""})
	}
	err = rows.Err()
	if err != nil {
		return err
	}
	return nil
}
