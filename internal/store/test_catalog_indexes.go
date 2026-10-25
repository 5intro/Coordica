package store

import "database/sql"

func initializeTestCatalog(db *sql.DB) error {
	_, err := db.Exec(`CREATE INDEX IF NOT EXISTS test_verification_task ON records(
 json_extract(data,'$.verification.suite.id'),json_extract(data,'$.verification.commit'))
 WHERE kind='task' AND json_type(data,'$.verification')='object';
 CREATE INDEX IF NOT EXISTS test_verification_run ON records(
 json_extract(data,'$.task_id'),json_extract(data,'$.generation') DESC) WHERE kind='run';`)
	return err
}
