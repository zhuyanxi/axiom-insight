package sqlfixture

import (
	"database/sql"
)

func Run(db *sql.DB, tx *sql.Tx) {
	db.Query("SELECT id FROM orders")
	tx.Exec("UPDATE orders SET status = 1")
}
