package database

import (
	"database/sql"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/dmage/ci-results/testgrid"
	lru "github.com/hashicorp/golang-lru"
	_ "github.com/mattn/go-sqlite3"
	"k8s.io/klog/v2"
)

type JobTags struct {
	Platform string
	Mod      string
	TestType string
	Sippy    []string
}

type errNotFound struct {
	msg string
}

func newErrNotFound(format string, args ...interface{}) errNotFound {
	return errNotFound{
		msg: fmt.Sprintf(format, args...),
	}
}

func (e errNotFound) Error() string {
	return e.msg
}

func IsNotFound(err error) bool {
	_, ok := err.(errNotFound)
	return ok
}

type buildKey struct {
	JobID  int64
	Number string
}

type sqlConn interface {
	Prepare(query string) (*sql.Stmt, error)
	Query(query string, args ...interface{}) (*sql.Rows, error)
	Exec(query string, args ...interface{}) (sql.Result, error)
}

type dbImpl struct {
	sqlConn

	jobsCache   *lru.Cache
	buildsCache *lru.Cache
	testsCache  *lru.Cache

	selectJobStmt        *sql.Stmt
	insertJobStmt        *sql.Stmt
	selectBuildStmt      *sql.Stmt
	insertBuildStmt      *sql.Stmt
	selectTestStmt       *sql.Stmt
	insertTestStmt       *sql.Stmt
	selectTestResultStmt *sql.Stmt
	insertTestResultStmt *sql.Stmt
}

type DB struct {
	dbImpl
	db *sql.DB
}

type Tx struct {
	dbImpl
	tx *sql.Tx
}

func Open(dsn string) (*DB, error) {
	sqlDB, err := sql.Open("sqlite3", dsn)
	if err != nil {
		return nil, fmt.Errorf("unable to open database: %w", err)
	}

	db := &DB{
		dbImpl: dbImpl{sqlConn: sqlDB},
		db:     sqlDB,
	}

	err = db.init()
	if err != nil {
		sqlDB.Close()
		return nil, fmt.Errorf("unable to initialize database: %w", err)
	}

	err = db.initStmts()

	return db, err
}

func OpenDefault() (*DB, error) {
	return Open("./results.db?_journal_mode=WAL&_cache_size=-10000")
}

func (db *DB) Begin() (*Tx, error) {
	tx, err := db.db.Begin()
	if err != nil {
		return nil, err
	}

	impl := db.dbImpl
	impl.sqlConn = tx
	return &Tx{
		dbImpl: impl,
		tx:     tx,
	}, impl.initStmts()
}

func (db *DB) Close() error {
	return db.db.Close()
}

func (tx *Tx) Commit() error {
	return tx.tx.Commit()
}

func (db *dbImpl) init() error {
	var err error

	db.jobsCache, err = lru.New(20)
	if err != nil {
		return err
	}

	db.buildsCache, err = lru.New(100)
	if err != nil {
		return err
	}

	db.testsCache, err = lru.New(5000)
	if err != nil {
		return err
	}

	initStatements := []string{
		`create table if not exists jobs (
			id integer not null primary key,
			name text not null,
			platform text not null,
			mod text not null,
			testtype text not null
		);`,
		`create table if not exists jobs_sippy_tags (
			job_id integer not null,
			tag text not null
		);`,
		`create table if not exists builds (
			id integer not null primary key,
			job_id integer not null,
			number text not null,
			timestamp integer not null,
			status integer not null
		);`,
		`create table if not exists tests (
			id integer not null primary key,
			name text not null
		);`,
		`create table if not exists test_results (
			build_id integer not null,
			test_id integer not null,
			status integer not null
		);`,
		`create unique index if not exists jobs_name on jobs (name);`,
		`create unique index if not exists jobs_sippy_tags_job_tag on jobs_sippy_tags (job_id, tag);`,
		`create unique index if not exists builds_job_number on builds (job_id, number);`,
		`create unique index if not exists tests_name on tests (name);`,
		`create unique index if not exists test_results_build_test on test_results (build_id, test_id);`,
	}
	for _, stmt := range initStatements {
		_, err := db.Exec(stmt)
		if err != nil {
			return fmt.Errorf("%s: %s", err, stmt)
		}
	}

	return nil
}

func (db *dbImpl) initStmts() error {
	var err error

	db.selectJobStmt, err = db.Prepare("select id from jobs where name = ?")
	if err != nil {
		return err
	}

	db.insertJobStmt, err = db.Prepare("insert or ignore into jobs (name, platform, mod, testtype) values (?, ?, ?, ?)")
	if err != nil {
		return err
	}

	db.selectBuildStmt, err = db.Prepare("select id from builds where job_id = ? and number = ?")
	if err != nil {
		return err
	}

	db.insertBuildStmt, err = db.Prepare("insert or ignore into builds (job_id, number, timestamp, status) values (?, ?, ?, ?)")
	if err != nil {
		return err
	}

	db.selectTestStmt, err = db.Prepare("select id from tests where name = ?")
	if err != nil {
		return err
	}

	db.insertTestStmt, err = db.Prepare("insert or ignore into tests (name) values (?)")
	if err != nil {
		return err
	}

	db.selectTestResultStmt, err = db.Prepare("select 1 from test_results where build_id = ? and test_id = ?")
	if err != nil {
		return err
	}

	db.insertTestResultStmt, err = db.Prepare("insert or ignore into test_results (build_id, test_id, status) values (?, ?, ?)")
	if err != nil {
		return err
	}

	return nil
}

func (db *dbImpl) FindJob(name string) (id int64, err error) {
	obj, ok := db.jobsCache.Get(name)
	if ok {
		return obj.(int64), nil
	}

	row := db.selectJobStmt.QueryRow(name)
	if err = row.Scan(&id); err == sql.ErrNoRows {
		return 0, newErrNotFound("job %s does not exist", name)
	} else if err != nil {
		return 0, err
	}

	db.jobsCache.Add(name, id)
	return id, nil
}

func (db *dbImpl) InsertJob(name string, tags JobTags) (int64, error) {
	result, err := db.insertJobStmt.Exec(name, tags.Platform, tags.Mod, tags.TestType)
	if err != nil {
		return 0, err
	}

	id, err := result.LastInsertId()
	if err != nil {
		return 0, err
	}

	db.jobsCache.Add(name, id)
	/* This is a very lazy way to do it { */
	for _, sippyTag := range tags.Sippy {
		_, err := db.Exec("insert into jobs_sippy_tags (job_id, tag) values (?, ?)", id, sippyTag)
		if err != nil {
			return id, err
		}
	}
	/* } */
	return id, nil
}

func (db *dbImpl) UpsertBuild(jobID int64, number string, timestamp int64, status int) (int64, error) {
	obj, ok := db.buildsCache.Get(buildKey{JobID: jobID, Number: number})
	if ok {
		return obj.(int64), nil
	}

	var id int64
	row := db.selectBuildStmt.QueryRow(jobID, number)
	err := row.Scan(&id)
	if err == nil {
		db.buildsCache.Add(buildKey{JobID: jobID, Number: number}, id)
		return id, nil
	}
	if err != sql.ErrNoRows {
		return 0, err
	}

	result, err := db.insertBuildStmt.Exec(jobID, number, timestamp, status)
	if err != nil {
		return 0, err
	}
	id, err = result.LastInsertId()
	if err != nil {
		return 0, err
	}
	db.buildsCache.Add(buildKey{JobID: jobID, Number: number}, id)
	return id, nil
}

func (db *dbImpl) UpsertTest(name string) (int64, error) {
	obj, ok := db.testsCache.Get(name)
	if ok {
		return obj.(int64), nil
	}

	var id int64
	row := db.selectTestStmt.QueryRow(name)
	err := row.Scan(&id)
	if err == nil {
		db.testsCache.Add(name, id)
		return id, nil
	}
	if err != sql.ErrNoRows {
		return 0, err
	}

	result, err := db.insertTestStmt.Exec(name)
	if err != nil {
		return 0, err
	}
	id, err = result.LastInsertId()
	if err != nil {
		return 0, err
	}
	db.testsCache.Add(name, id)
	return id, nil
}

func (db *dbImpl) UpsertTestResult(buildID, testID int64, status testgrid.TestStatus) error {
	var i int
	row := db.selectTestResultStmt.QueryRow(buildID, testID)
	err := row.Scan(&i)
	if err == nil {
		return nil
	}

	_, err = db.insertTestResultStmt.Exec(buildID, testID, status)
	return err
}

type StatsValues struct {
	Pass int `json:"pass"`
	Fail int `json:"fail"`
}

type StatsRow struct {
	Columns []string      `json:"columns"`
	Values  []StatsValues `json:"values"`
}

type Stats struct {
	Data []*StatsRow `json:"data"`
}

func (db *dbImpl) findJobIDsByFilter(filter string) ([]int64, error) {
	tagRe := regexp.MustCompile("^[a-z0-9-]+$")
	terms := strings.Split(filter, " ")

	joins := ""
	conds := ""
	c := 0
	for _, term := range terms {
		if len(term) == 0 {
			continue
		}
		if !tagRe.MatchString(term) {
			return nil, fmt.Errorf("invalid filter term: %s", term)
		}
		c++
		if term[0] == '-' {
			term = term[1:]
			if joins != "" {
				joins += " "
			}
			joins += fmt.Sprintf(
				"LEFT JOIN jobs_sippy_tags jst%d ON jst%d.job_id = j.id AND jst%d.tag = \"%s\"",
				c, c, c, term,
			)
			if conds != "" {
				conds += " AND "
			}
			conds += fmt.Sprintf("jst%d.job_id IS NULL", c)
		} else {
			if joins != "" {
				joins += " "
			}
			joins += fmt.Sprintf(
				"JOIN jobs_sippy_tags jst%d ON jst%d.job_id = j.id AND jst%d.tag = \"%s\"",
				c, c, c, term,
			)
		}
	}
	if conds != "" {
		conds = "WHERE " + conds
	}

	var result []int64
	rows, err := db.Query("SELECT j.id FROM jobs j " + joins + " " + conds)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var id int64
		err := rows.Scan(&id)
		if err != nil {
			return nil, err
		}

		result = append(result, id)
	}
	return result, nil
}

func (db *dbImpl) BuildStats(columns string, filter string, periods string) (*Stats, error) {
	results := Stats{
		Data: []*StatsRow{},
	}
	resultsByTag := map[string]*StatsRow{}
	now := time.Now()
	conds := ""
	if filter != "" {
		jobIDs, err := db.findJobIDsByFilter(filter)
		if err != nil {
			return nil, err
		}
		if len(jobIDs) == 0 {
			conds = "WHERE 0 = 1"
		} else {
			conds = "WHERE j.id IN ("
			for i, id := range jobIDs {
				if i != 0 {
					conds += ","
				}
				conds += strconv.FormatInt(id, 10)
			}
			conds += ")"
		}
	}
	sel := "jst.tag"
	statusField := "b.status"
	joins := "JOIN jobs_sippy_tags jst ON jst.job_id = j.id"
	if columns == "name" {
		sel = "j.name"
		joins = ""
	} else if columns == "test" {
		sel = "t.name"
		statusField = "tr.status"
		joins += " JOIN test_results tr ON tr.build_id = b.id JOIN tests t ON t.id = tr.test_id"
	}
	if conds == "" {
		conds = "WHERE "
	} else {
		conds += " AND "
	}
	conds += "b.timestamp >= ?"
	p := strings.Split(periods, ",")
	if len(p) != 2 {
		return nil, fmt.Errorf("periods should be <number>,<number>, got %s", periods)
	}
	p1, err := strconv.ParseInt(p[0], 10, 0)
	if err != nil {
		return nil, err
	}
	p2, err := strconv.ParseInt(p[1], 10, 0)
	if err != nil {
		return nil, err
	}
	rows, err := db.Query(
		"SELECT "+sel+", "+statusField+", SUM(? <= b.timestamp), SUM(? <= b.timestamp AND b.timestamp < ?) FROM builds b JOIN jobs j ON j.id = b.job_id "+joins+" "+conds+" GROUP BY "+sel+", "+statusField,
		(now.Unix()-86400*p1)*1000,
		(now.Unix()-86400*(p1+p2))*1000, (now.Unix()-86400*p1)*1000,
		(now.Unix()-86400*(p1+p2))*1000,
	)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var tag string
		var status int
		var currWeek int
		var prevWeek int
		err := rows.Scan(&tag, &status, &currWeek, &prevWeek)
		if err != nil {
			return nil, err
		}

		row, ok := resultsByTag[tag]
		if !ok {
			row = &StatsRow{
				Columns: []string{tag},
				Values: []StatsValues{
					{}, {},
				},
			}
			results.Data = append(results.Data, row)
			resultsByTag[tag] = row
		}

		if statusField == "tr.status" {
			if status == int(testgrid.TestStatusPass) || status == int(testgrid.TestStatusPassWithSkips) || status == int(testgrid.TestStatusFlaky) {
				row.Values[0].Pass += currWeek
				row.Values[1].Pass += prevWeek
			} else if status == int(testgrid.TestStatusFail) {
				row.Values[0].Fail += currWeek
				row.Values[1].Fail += prevWeek
			} else {
				klog.Infof("unexpected test status: %d", status)
			}
		} else {
			if status == 1 {
				row.Values[0].Pass = currWeek
				row.Values[1].Pass = prevWeek
			} else if status == 2 {
				row.Values[0].Fail = currWeek
				row.Values[1].Fail = prevWeek
			}
		}
	}
	return &results, err
}
