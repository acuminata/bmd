package store

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	_ "modernc.org/sqlite"

	"bmd/internal/model"
)

// DB wraps the sqlite database.
type DB struct {
	SQL *sql.DB
}

// NoticeRow is a stored record with S3 photo references.
type NoticeRow struct {
	Dataset             string
	ApplicationNumber   string
	SourceID            int64
	BrideGroomsName     string
	BrideGroomsPhoto    string
	BrideGroomsPhotoS3  string
	BridesName          string
	BridesPhoto         string
	BridesPhotoS3       string
	CreatedBy           string
	DateOfMarriage      string
	DateOfNotice        string
	PlaceOfMarriage     string
	SubmittedAt         string
	PhotoUploadedAt     string
}

// Open creates/opens the sqlite file and migrates the schema.
func Open(path string) (*DB, error) {
	if path == "" {
		path = "bmd.db"
	}
	dsn := fmt.Sprintf("file:%s?_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)&_pragma=foreign_keys(1)", path)
	sqldb, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, err
	}
	sqldb.SetMaxOpenConns(1)
	if err := sqldb.Ping(); err != nil {
		return nil, err
	}
	db := &DB{SQL: sqldb}
	if err := db.Migrate(); err != nil {
		sqldb.Close()
		return nil, err
	}
	return db, nil
}

// Migrate creates the notices table. application_number dedupes via
// UNIQUE(dataset, application_number); inserts use OR IGNORE.
func (d *DB) Migrate() error {
	_, err := d.SQL.Exec(`CREATE TABLE IF NOT EXISTS notices (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		dataset TEXT NOT NULL,
		application_number TEXT NOT NULL,
		source_id INTEGER NOT NULL DEFAULT 0,
		bride_grooms_name TEXT NOT NULL DEFAULT '',
		bride_grooms_photo TEXT NOT NULL DEFAULT '',
		bride_grooms_photo_s3 TEXT NOT NULL DEFAULT '',
		brides_name TEXT NOT NULL DEFAULT '',
		brides_photo TEXT NOT NULL DEFAULT '',
		brides_photo_s3 TEXT NOT NULL DEFAULT '',
		created_by TEXT NOT NULL DEFAULT '',
		date_of_marriage TEXT NOT NULL DEFAULT '',
		date_of_notice TEXT NOT NULL DEFAULT '',
		place_of_marriage TEXT NOT NULL DEFAULT '',
		submitted_at TEXT NOT NULL DEFAULT '',
		raw_json TEXT NOT NULL DEFAULT '',
		fetched_at TEXT NOT NULL DEFAULT '',
		photo_uploaded_at TEXT NOT NULL DEFAULT '',
		UNIQUE(dataset, application_number)
	);
	CREATE INDEX IF NOT EXISTS idx_notices_dataset ON notices(dataset);
	CREATE INDEX IF NOT EXISTS idx_notices_appno ON notices(application_number);`)
	return err
}

// InsertIgnore stores an entry; ok=false means the application_number
// already existed for that dataset and the row was left untouched.
func (d *DB) InsertIgnore(dataset string, e model.Entry) (ok bool, err error) {
	raw, _ := json.Marshal(e)
	res, err := d.SQL.Exec(`INSERT OR IGNORE INTO notices
		(dataset, application_number, source_id, bride_grooms_name, bride_grooms_photo,
		 brides_name, brides_photo, created_by, date_of_marriage, date_of_notice,
		 place_of_marriage, submitted_at, raw_json, fetched_at)
		VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		dataset, e.ApplicationNumber, e.ID, e.BrideGroomsName, e.BrideGroomsPhoto,
		e.BridesName, e.BridesPhoto, e.CreatedBy, e.DateOfMarriage, e.DateOfNotice,
		e.PlaceOfMarriage, e.SubmittedAt, string(raw), time.Now().UTC().Format(time.RFC3339),
	)
	if err != nil {
		return false, err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return false, err
	}
	return n == 1, nil
}

// UpdatePhotos records the S3 keys/URLs after a successful upload.
func (d *DB) UpdatePhotos(dataset, applicationNumber, groomS3, brideS3 string) error {
	_, err := d.SQL.Exec(`UPDATE notices SET bride_grooms_photo_s3=?, brides_photo_s3=?, photo_uploaded_at=?
		WHERE dataset=? AND application_number=?`,
		groomS3, brideS3, time.Now().UTC().Format(time.RFC3339), dataset, applicationNumber)
	return err
}

// Counts returns per-dataset row totals.
func (d *DB) Counts() (map[string]int, error) {
	rows, err := d.SQL.Query(`SELECT dataset, COUNT(*) FROM notices GROUP BY dataset`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]int{}
	for rows.Next() {
		var ds string
		var n int
		if err := rows.Scan(&ds, &n); err != nil {
			return nil, err
		}
		out[ds] = n
	}
	return out, rows.Err()
}

// Close closes the database.
func (d *DB) Close() error { return d.SQL.Close() }
