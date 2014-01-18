package mysql

import (
	"fmt"
	"database/sql"
	"encoding/json"
	_ "github.com/go-sql-driver/mysql"
	"github.com/percona/cloud-tools/mm"
	"github.com/percona/cloud-tools/pct"
	"strconv"
	"strings"
	"time"
)

type Monitor struct {
	logger *pct.Logger
	// --
	config         *Config
	ticker         pct.Ticker
	collectionChan chan *mm.Collection
	// --
	conn          *sql.DB
	connected     bool
	connectedChan chan bool
	status        *pct.Status
	backoff       *pct.Backoff
	sync          *pct.SyncChan
}

func NewMonitor(logger *pct.Logger) *Monitor {
	m := &Monitor{
		logger: logger,
		// --
		connectedChan: make(chan bool, 1),
		status:        pct.NewStatus([]string{"mysql"}),
		backoff:       pct.NewBackoff(5 * time.Second),
		sync:          pct.NewSyncChan(),
	}
	return m
}

/////////////////////////////////////////////////////////////////////////////
// Interface
/////////////////////////////////////////////////////////////////////////////

// @goroutine[0]
func (m *Monitor) Start(config []byte, ticker pct.Ticker, collectionChan chan *mm.Collection) error {
	if m.config != nil {
		return pct.ServiceIsRunningError{"mysql-monitor"}
	}

	c := &Config{}
	if err := json.Unmarshal(config, c); err != nil {
		return err
	}
	m.config = c
	m.ticker = ticker
	m.collectionChan = collectionChan

	m.status.Update("mysql", "Synchronizing")
	m.ticker.Sync(time.Now().UnixNano())

	go m.run()

	return nil
}

// @goroutine[0]
func (m *Monitor) Stop() {
	if m.config == nil {
		return // already stopped
	}

	// Stop run().  When it returns, it updates status to "Stopped".
	m.status.Update("mysql", "Stopping")
	m.ticker.Stop()
	m.sync.Stop()
	m.sync.Wait()

	m.config = nil // no config if not running

	// Do not update status to "Stopped" here; run() does that on return.
}

// @goroutine[0]
func (m *Monitor) Status() map[string]string {
	return m.status.All()
}

/////////////////////////////////////////////////////////////////////////////
// Implementation
/////////////////////////////////////////////////////////////////////////////

// @goroutine[1]
func (m *Monitor) connect() {

	// Close/release previous connection, if any.
	if m.conn != nil {
		m.conn.Close()
	}

	// Try forever to connect to MySQL...
	for m.conn == nil {

		// Wait between connect attempts.
		t := m.backoff.Wait()
		m.status.Update("mysql", fmt.Sprintf("Connect wait %s", t))
		time.Sleep(t)
		m.status.Update("mysql", "Connecting")

		// Open connection to MySQL but...
		db, err := sql.Open("mysql", m.config.DSN)
		if err != nil {
			m.logger.Error("sql.Open: ", err)
			continue
		}

		// ...try to use the connection for real.
		if err := db.Ping(); err != nil {
			// Connection failed.  Wrong username or password?
			m.logger.Warn("db.Ping: ", err)
			db.Close()
			continue
		}

		// Connected
		m.conn = db
		m.backoff.Success()

		// Set global vars we need.  If these fail, that's ok: they won't work,
		// but don't let that stop us from collecting other metrics.
		if m.config.InnoDB != "" {
			sql := `SET GLOBAL innodb_monitor_enable = "` + m.config.InnoDB + `"`
			if _, err := db.Exec(sql); err != nil {
				m.logger.Error(sql, err)
			}
		}

		if m.config.Userstat {
			// 5.1.49 <= v <= 5.5.10: SET GLOBAL userstat_running=ON
			// 5.5.10 <  v:           SET GLOBAL userstat=ON
			sql := "SET GLOBAL userstat=ON"
			if _, err := db.Exec(sql); err != nil {
				m.logger.Error(sql, err)
			}
		}

		// Tell run() goroutine that it can try to collect metrics.
		// If connection is lost, it will call us again.
		m.status.Update("mysql", "Connected")
		m.connectedChan <- true
	}
}

// @goroutine[2]
func (m *Monitor) run() {
	go m.connect()
	defer func() {
		if m.conn != nil {
			m.conn.Close()
		}
		m.status.Update("mysql", "Stopped")
		m.sync.Done()
	}()

	prefix := "mysql"
	if m.config.InstanceName != "" {
		prefix += "/" + m.config.InstanceName
	}

	for {
		select {
		case now := <-m.ticker.TickerChan():
			if m.connected {
				m.status.Update("mysql", "Running")

				c := &mm.Collection{
					StartTs: now.Unix(),
					Metrics: []mm.Metric{},
				}

				// Get collection of metrics.
				m.GetShowStatusMetrics(m.conn, prefix, c)
				if m.config.InnoDB != "" {
					m.GetInnoDBMetrics(m.conn, prefix, c)
				}
				if m.config.Userstat {
					m.GetTableStatMetrics(m.conn, prefix, c, m.config.UserstatIgnoreDb)
					m.GetIndexStatMetrics(m.conn, prefix, c, m.config.UserstatIgnoreDb)
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
					m.logger.Debug("No metrics")
				}

				m.status.Update("mysql", "Ready")
			} else {
				m.logger.Debug("Not connected")
			}
		case connected := <-m.connectedChan:
			m.connected = connected
			if connected {
				m.status.Update("mysql", "Ready")
			} else {
				go m.connect()
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
func (m *Monitor) GetShowStatusMetrics(conn *sql.DB, prefix string, c *mm.Collection) error {
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

		metricType, ok := m.config.Status[statName]
		if !ok {
			continue // not collecting this stat
		}

		metricName := prefix + "/" + strings.ToLower(statName)
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
func (m *Monitor) GetInnoDBMetrics(conn *sql.DB, prefix string, c *mm.Collection) error {
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

		metricName := prefix + "/innodb/" + strings.ToLower(statSubsystem) + "/" + strings.ToLower(statName)
		metricValue, err := strconv.ParseFloat(statCount, 64)
		if err != nil {
			metricValue = 0.0
		}
		var metricType byte
		if statType == "value" {
			metricType = mm.NUMBER
		} else {
			metricType = mm.COUNTER
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
func (m *Monitor) GetTableStatMetrics(conn *sql.DB, prefix string, c *mm.Collection, ignoreDb string) error {
	/*
	   SELECT * FROM INFORMATION_SCHEMA.TABLE_STATISTICS;
	   +--------------+-------------+-----------+--------------+------------------------+
	   | TABLE_SCHEMA | TABLE_NAME  | ROWS_READ | ROWS_CHANGED | ROWS_CHANGED_X_INDEXES |
	*/
	tableStatSQL := "SELECT TABLE_SCHEMA,TABLE_NAME,ROWS_READ,ROWS_CHANGED,ROWS_CHANGED_X_INDEXES FROM INFORMATION_SCHEMA.TABLE_STATISTICS"
	if ignoreDb != "" {
		tableStatSQL = tableStatSQL + " WHERE TABLE_SCHEMA NOT LIKE '" + ignoreDb + "'"
	}
	rows, err := conn.Query(tableStatSQL)
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
			Name:  prefix + "/db." + tableSchema + "/t." + tableName + "/rows_read",
			Type: mm.NUMBER,
			Number: float64(rowsRead),
		})
		c.Metrics = append(c.Metrics, mm.Metric{
			Name:  prefix + "/db." + tableSchema + "/t." + tableName + "/rows_changed",
			Type: mm.NUMBER,
			Number: float64(rowsChanged),
		})
		c.Metrics = append(c.Metrics, mm.Metric{
			Name:  prefix + "/db." + tableSchema + "/t." + tableName + "/rows_changed_x_indexes",
			Type: mm.NUMBER,
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
func (m *Monitor) GetIndexStatMetrics(conn *sql.DB, prefix string, c *mm.Collection, ignoreDb string) error {
	/*
	   SELECT * FROM INFORMATION_SCHEMA.INDEX_STATISTICS;
	   +--------------+-------------+------------+-----------+
	   | TABLE_SCHEMA | TABLE_NAME  | INDEX_NAME | ROWS_READ | select * from INFORMATION_SCHEMA.INDEX_STATISTICS;
	   +--------------+-------------+------------+-----------+
	*/
	indexStatSQL := "SELECT TABLE_SCHEMA,TABLE_NAME,INDEX_NAME,ROWS_READ FROM INFORMATION_SCHEMA.INDEX_STATISTICS"
	if ignoreDb != "" {
		indexStatSQL = indexStatSQL + " WHERE TABLE_SCHEMA NOT LIKE '" + ignoreDb + "'"
	}
	rows, err := conn.Query(indexStatSQL)
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

		metricName := prefix + "/db." + tableSchema + "/t." + tableName + "/idx." + indexName + "/rows_read"
		metricValue := float64(rowsRead)
		c.Metrics = append(c.Metrics, mm.Metric{metricName, mm.NUMBER, metricValue, ""})
	}
	err = rows.Err()
	if err != nil {
		return err
	}
	return nil
}
