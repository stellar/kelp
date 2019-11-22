package plugins

import (
	"database/sql"
	"fmt"
	"log"
	"time"

	_ "github.com/lib/pq"
	"github.com/stellar/kelp/api"
	"github.com/stellar/kelp/model"
	"github.com/stellar/kelp/support/postgresdb"
	"github.com/stellar/kelp/support/utils"
)

const dateFormatString = "2006/01/02"
const sqlTableCreate = "CREATE TABLE IF NOT EXISTS trades (txid TEXT PRIMARY KEY, date_utc VARCHAR(10), timestamp_millis INTEGER, base TEXT, quote TEXT, action TEXT, type TEXT, counter_price REAL, base_volume REAL, counter_cost REAL, fee REAL)"
const sqlInsert = "INSERT INTO trades (txid, date_utc, timestamp_millis, base, quote, action, type, counter_price, base_volume, counter_cost, fee) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)"

var sqlIndexes = []string{
	"CREATE INDEX IF NOT EXISTS date ON trades (date_utc, base, quote)",
}

// FillDBWriter is a FillHandler that writes fills to a SQL database
type FillDBWriter struct {
	db *sql.DB
}

var _ api.FillHandler = &FillDBWriter{}

// MakeFillDBWriter is a factory method
func MakeFillDBWriter(postgresDbConfig *postgresdb.Config) (api.FillHandler, error) {
	dbCreated, e := postgresdb.CreateDatabaseIfNotExists(postgresDbConfig)
	if e != nil {
		return nil, fmt.Errorf("error when creating database from config (%+v), created=%v: %s", *postgresDbConfig, dbCreated, e)
	}
	if dbCreated {
		log.Printf("created database '%s'", postgresDbConfig.GetDbName())
	} else {
		log.Printf("did not create db '%s' because it already exists", postgresDbConfig.GetDbName())
	}

	db, e := sql.Open("postgres", postgresDbConfig.MakeConnectString())
	if e != nil {
		return nil, fmt.Errorf("could not open database: %s", e)
	}
	// don't defer db.Close() here becuase we want it open for the life of the application for now

	statement, e := db.Prepare(sqlTableCreate)
	if e != nil {
		return nil, fmt.Errorf("could not prepare sql statement: %s", e)
	}
	_, e = statement.Exec()
	if e != nil {
		return nil, fmt.Errorf("could not execute sql create table statement: %s", e)
	}

	for i, sqlIndexCreate := range sqlIndexes {
		statement, e = db.Prepare(sqlIndexCreate)
		if e != nil {
			return nil, fmt.Errorf("could not prepare sql statement to create index (i=%d): %s", i, e)
		}
		_, e = statement.Exec()
		if e != nil {
			return nil, fmt.Errorf("could not execute sql statement to create index (i=%d): %s", i, e)
		}
	}

	fdbw := &FillDBWriter{db: db}
	log.Printf("made FillDBWriter with db config: %s\n", postgresDbConfig.MakeConnectString())
	return fdbw, nil
}

// HandleFill impl.
func (f *FillDBWriter) HandleFill(trade model.Trade) error {
	statement, e := f.db.Prepare(sqlInsert)
	if e != nil {
		return fmt.Errorf("could not prepare sql insert values statement: %s", e)
	}

	txid := utils.CheckedString(trade.TransactionID)
	timeSeconds := trade.Timestamp.AsInt64() / 1000
	date := time.Unix(timeSeconds, 0).UTC()
	dateString := date.Format(dateFormatString)

	_, e = statement.Exec(
		txid,
		dateString,
		utils.CheckedString(trade.Timestamp),
		string(trade.Pair.Base),
		string(trade.Pair.Quote),
		trade.OrderAction.String(),
		trade.OrderType.String(),
		f.checkedFloat(trade.Price),
		f.checkedFloat(trade.Volume),
		f.checkedFloat(trade.Cost),
		f.checkedFloat(trade.Fee),
	)
	if e != nil {
		return fmt.Errorf("could not execute sql insert values statement: %s", e)
	}
	log.Printf("wrote trade (txid=%s) to db\n", txid)
	return nil
}

func (f *FillDBWriter) checkedFloat(n *model.Number) interface{} {
	if n == nil {
		return nil
	}
	return n.AsFloat()
}
