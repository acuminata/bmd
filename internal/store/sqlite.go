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
	Kind                string
	ApplicationNumber   string
	SourceID            int64
	BrideGroomsName     string
	BrideGroomsPhoto    string
	BrideGroomsPhotoS3  string
	BridesName          string
	BridesPhoto         string
	BridesPhotoS3       string
	CreatedBy           string
	MarriageDate        string
	NoticeDate          string
	PlaceOfMarriage     string
	SubmittedAt         string
	Metadata            string
	ArchivedOn          string
}

// KindForDataset maps a fetched dataset to its notice kind:
// marriages (both parties Kenyan) -> domestic,
// impediments (No Impediment to Marriage) -> international.
func KindForDataset(dataset string) string {
	switch dataset {
	case "marriages", "marriage":
		return "domestic"
	case "impediments", "impediment":
		return "international"
	default:
		return dataset
	}
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

// Migrate creates the marriage_notices table and moves rows over from the
// legacy notices table when present, then drops the old table.
// application_number dedupes via UNIQUE(kind, application_number);
// inserts use OR IGNORE.
func (d *DB) Migrate() error {
	_, err := d.SQL.Exec(`CREATE TABLE IF NOT EXISTS marriage_notices (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		kind TEXT NOT NULL,
		application_number TEXT NOT NULL,
		source_id INTEGER NOT NULL DEFAULT 0,
		bride_grooms_name TEXT NOT NULL DEFAULT '',
		bride_grooms_photo TEXT NOT NULL DEFAULT '',
		bride_grooms_photo_s3 TEXT NOT NULL DEFAULT '',
		brides_name TEXT NOT NULL DEFAULT '',
		brides_photo TEXT NOT NULL DEFAULT '',
		brides_photo_s3 TEXT NOT NULL DEFAULT '',
		created_by TEXT NOT NULL DEFAULT '',
		marriage_date TEXT NOT NULL DEFAULT '',
		notice_date TEXT NOT NULL DEFAULT '',
		place_of_marriage TEXT NOT NULL DEFAULT '',
		submitted_at TEXT NOT NULL DEFAULT '',
		metadata TEXT NOT NULL DEFAULT '',
		archived_on TEXT NOT NULL DEFAULT '',
		UNIQUE(kind, application_number)
	);
	CREATE INDEX IF NOT EXISTS idx_marriage_notices_kind ON marriage_notices(kind);
	CREATE INDEX IF NOT EXISTS idx_marriage_notices_appno ON marriage_notices(application_number);`)
	if err != nil {
		return err
	}
	if !d.tableExists("notices") {
		return nil
	}
	_, err = d.SQL.Exec(`INSERT OR IGNORE INTO marriage_notices
		(kind, application_number, source_id, bride_grooms_name, bride_grooms_photo,
		 bride_grooms_photo_s3, brides_name, brides_photo, brides_photo_s3,
		 created_by, marriage_date, notice_date, place_of_marriage,
		 submitted_at, metadata, archived_on)
		SELECT
			CASE dataset WHEN 'marriages' THEN 'domestic' WHEN 'impediments' THEN 'international' ELSE dataset END,
			application_number, source_id, bride_grooms_name, bride_grooms_photo,
			bride_grooms_photo_s3, brides_name, brides_photo, brides_photo_s3,
			created_by, date_of_marriage, date_of_notice, place_of_marriage,
			submitted_at, raw_json, fetched_at
		FROM notices`)
	if err != nil {
		return fmt.Errorf("migrate legacy notices table: %w", err)
	}
	_, err = d.SQL.Exec(`DROP TABLE notices`)
	return err
}

func (d *DB) tableExists(name string) bool {
	var found string
	err := d.SQL.QueryRow(`SELECT name FROM sqlite_master WHERE type='table' AND name=?`, name).Scan(&found)
	return err == nil && found == name
}

// InsertIgnore stores an entry; ok=false means the application_number
// already existed for that kind and the row was left untouched.
func (d *DB) InsertIgnore(kind string, e model.Entry) (ok bool, err error) {
	raw, _ := json.Marshal(e)
	res, err := d.SQL.Exec(`INSERT OR IGNORE INTO marriage_notices
		(kind, application_number, source_id, bride_grooms_name, bride_grooms_photo,
		 brides_name, brides_photo, created_by, marriage_date, notice_date,
		 place_of_marriage, submitted_at, metadata, archived_on)
		VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		kind, e.ApplicationNumber, e.ID, e.BrideGroomsName, e.BrideGroomsPhoto,
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
func (d *DB) UpdatePhotos(kind, applicationNumber, groomS3, brideS3 string) error {
	_, err := d.SQL.Exec(`UPDATE marriage_notices SET bride_grooms_photo_s3=?, brides_photo_s3=?
		WHERE kind=? AND application_number=?`,
		groomS3, brideS3, kind, applicationNumber)
	return err
}

// Counts returns per-kind row totals.
func (d *DB) Counts() (map[string]int, error) {
	rows, err := d.SQL.Query(`SELECT kind, COUNT(*) FROM marriage_notices GROUP BY kind`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]int{}
	for rows.Next() {
		var k string
		var n int
		if err := rows.Scan(&k, &n); err != nil {
			return nil, err
		}
		out[k] = n
	}
	return out, rows.Err()
}

// Close closes the database.
func (d *DB) Close() error { return d.SQL.Close() }
