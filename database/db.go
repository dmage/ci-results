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
			dashboard text not null,
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
		`create        index if not exists test_results_test_id_status on test_results (test_id, status);`,
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

	db.insertJobStmt, err = db.Prepare("insert or ignore into jobs (name, dashboard, platform, mod, testtype) values (?, ?, ?, ?, ?)")
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

func (db *dbImpl) FindTest(testName string) (id int64, err error) {
	row := db.selectTestStmt.QueryRow(testName)
	if err = row.Scan(&id); err == sql.ErrNoRows {
		return 0, newErrNotFound("test %q does not exist", testName)
	} else if err != nil {
		return 0, err
	}
	return id, nil
}

func (db *dbImpl) InsertJob(name string, dashboard string, tags JobTags) (int64, error) {
	result, err := db.insertJobStmt.Exec(name, dashboard, tags.Platform, tags.Mod, tags.TestType)
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
	Pass  int `json:"pass"`
	Flake int `json:"flake"`
	Fail  int `json:"fail"`
}

type StatsRow struct {
	Columns []string      `json:"columns"`
	Values  []StatsValues `json:"values"`
}

type Stats struct {
	Data []*StatsRow `json:"data"`
}

func (db *dbImpl) findJobIDsByFilter(filter string) ([]int64, error) {
	tagRe := regexp.MustCompile("^[a-z0-9.-]+$")
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

type QueryBuilder struct {
	from         string
	columns      []string
	columnsPtrs  []interface{}
	selectParams []interface{}
	joins        []string
	joinParams   []interface{}
	condition    string
	whereParams  []interface{}
	groupby      []string
}

func (qb *QueryBuilder) Select(column string, output interface{}, params ...interface{}) {
	qb.columns = append(qb.columns, column)
	qb.columnsPtrs = append(qb.columnsPtrs, output)
	qb.selectParams = append(qb.selectParams, params...)
}

func (qb *QueryBuilder) Join(j string, params ...interface{}) {
	qb.joins = append(qb.joins, "JOIN "+j)
	qb.joinParams = append(qb.joinParams, params...)
}

func (qb *QueryBuilder) Where(cond string, params ...interface{}) {
	if qb.condition != "" {
		qb.condition += " AND "
	}
	qb.condition += cond
	qb.whereParams = append(qb.whereParams, params...)
}

func (qb *QueryBuilder) GroupBy(column string) {
	qb.groupby = append(qb.groupby, column)
}

func (qb *QueryBuilder) SQL() (string, []interface{}, []interface{}) {
	var params []interface{}

	q := "SELECT"
	for i, col := range qb.columns {
		if i != 0 {
			q += ","
		}
		q = q + " " + col
	}
	params = append(params, qb.selectParams...)

	q += " FROM " + qb.from

	for _, j := range qb.joins {
		q += " " + j
	}
	params = append(params, qb.joinParams...)

	if qb.condition != "" {
		q += " WHERE " + qb.condition
	}
	params = append(params, qb.whereParams...)

	if len(qb.groupby) > 0 {
		q += " GROUP BY"
		for i, col := range qb.groupby {
			if i != 0 {
				q += ","
			}
			q = q + " " + col
		}
	}

	return q, params, qb.columnsPtrs
}

func sqlInt64List(a []int64) string {
	var s string
	for i, num := range a {
		if i != 0 {
			s += ","
		}
		s += strconv.FormatInt(num, 10)
	}
	return s
}

func (db *dbImpl) BuildStats(columns string, filter string, periods string, testName string) (*Stats, error) {
	now := time.Now()

	results := Stats{
		Data: []*StatsRow{},
	}
	resultsByTag := map[string]*StatsRow{}

	var query QueryBuilder
	query.from = "builds b"
	query.Join("jobs j ON j.id = b.job_id")

	if filter != "" {
		jobIDs, err := db.findJobIDsByFilter(filter)
		if err != nil {
			return nil, err
		}
		if len(jobIDs) == 0 {
			return &results, nil
		}
		query.Where("j.id IN (" + sqlInt64List(jobIDs) + ")")
	}

	var columnsPtrs []*string
	statusField := "b.status"
	for _, col := range strings.Split(columns, ",") {
		switch col {
		case "sippytags":
			var val string
			query.Join("jobs_sippy_tags jst ON jst.job_id = j.id")
			query.Select("jst.tag", &val)
			query.GroupBy("jst.tag")
			columnsPtrs = append(columnsPtrs, &val)
		case "name":
			var val string
			query.Select("j.name", &val)
			query.GroupBy("j.name")
			columnsPtrs = append(columnsPtrs, &val)
		case "dashboard":
			var val string
			query.Select("j.dashboard", &val)
			query.GroupBy("j.dashboard")
			columnsPtrs = append(columnsPtrs, &val)
		case "test":
			var val string
			statusField = "tr.status"
			query.Join("test_results tr ON tr.build_id = b.id")
			query.Join("tests t ON t.id = tr.test_id")
			query.Select("t.name", &val)
			query.GroupBy("t.name")
			columnsPtrs = append(columnsPtrs, &val)
		default:
			return nil, fmt.Errorf("unknown column %s", col)
		}
	}

	if testName != "" {
		testID, err := db.FindTest(testName)
		if IsNotFound(err) {
			return &results, nil
		} else if err != nil {
			return nil, err
		}
		if statusField == "tr.status" {
			query.Where("tr.test_id = ?", testID)
		} else {
			statusField = "tr.status"
			query.Join("test_results tr ON tr.build_id = b.id AND tr.test_id = ?", testID)
		}
	}

	var status int
	query.Select(statusField, &status)
	query.GroupBy(statusField)

	var periodsPtrs []*int
	var days int64
	for _, per := range strings.Split(periods, ",") {
		p, err := strconv.ParseInt(per, 10, 0)
		if err != nil {
			return nil, err
		}
		var val int
		if days == 0 {
			query.Select("SUM(? <= b.timestamp)", &val, (now.Unix()-86400*p)*1000)
		} else {
			query.Select("SUM(? <= b.timestamp AND b.timestamp < ?)", &val, (now.Unix()-86400*(days+p))*1000, (now.Unix()-86400*days)*1000)
		}
		periodsPtrs = append(periodsPtrs, &val)
		days += p
	}
	query.Where("b.timestamp >= ?", (now.Unix()-86400*days)*1000)

	sql, params, scanParams := query.SQL()

	rows, err := db.Query(sql, params...)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		err := rows.Scan(scanParams...)
		if err != nil {
			return nil, err
		}

		key := ""
		columnsValues := []string{}
		for _, p := range columnsPtrs {
			key += "/" + *p
			columnsValues = append(columnsValues, *p)
		}

		row, ok := resultsByTag[key]
		if !ok {
			row = &StatsRow{
				Columns: columnsValues,
				Values:  make([]StatsValues, len(periodsPtrs)),
			}
			results.Data = append(results.Data, row)
			resultsByTag[key] = row
		}

		if statusField == "tr.status" {
			if status == int(testgrid.TestStatusPass) || status == int(testgrid.TestStatusPassWithSkips) {
				for i, p := range periodsPtrs {
					row.Values[i].Pass += *p
				}
			} else if status == int(testgrid.TestStatusFlaky) {
				for i, p := range periodsPtrs {
					row.Values[i].Flake += *p
				}
			} else if status == int(testgrid.TestStatusFail) {
				for i, p := range periodsPtrs {
					row.Values[i].Fail += *p
				}
			} else {
				klog.Infof("unexpected test status: %d", status)
			}
		} else {
			if status == 1 {
				for i, p := range periodsPtrs {
					row.Values[i].Pass += *p
				}
			} else if status == 2 {
				for i, p := range periodsPtrs {
					row.Values[i].Fail += *p
				}
			}
		}
	}
	return &results, err
}
