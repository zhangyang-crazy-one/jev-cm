package store

import (
	"crypto/sha256"
	"database/sql"
	"database/sql/driver"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"unicode"

	sqlite "modernc.org/sqlite"
)

const schema = `
CREATE TABLE IF NOT EXISTS elision (
    pointer TEXT PRIMARY KEY,
    original BLOB NOT NULL,
    sha256 TEXT NOT NULL
);
CREATE TABLE IF NOT EXISTS memory (
    id INTEGER PRIMARY KEY,
    collection TEXT NOT NULL,
    source_id TEXT NOT NULL,
    text TEXT NOT NULL,
    sha256 TEXT NOT NULL,
    probability REAL,
    model_version TEXT,
    cwd TEXT,
    session_id TEXT,
    role TEXT
);
CREATE VIRTUAL TABLE IF NOT EXISTS memory_fts USING fts5(
    text,
    content='memory',
    content_rowid='id',
    tokenize='unicode61'
);
CREATE TRIGGER IF NOT EXISTS memory_ai AFTER INSERT ON memory BEGIN
    INSERT INTO memory_fts(rowid, text) VALUES (new.id, jev_index_text(new.text));
END;
CREATE TRIGGER IF NOT EXISTS memory_ad AFTER DELETE ON memory BEGIN
    INSERT INTO memory_fts(memory_fts, rowid, text) VALUES ('delete', old.id, jev_index_text(old.text));
END;
CREATE TABLE IF NOT EXISTS memory_edge (
    src INTEGER NOT NULL REFERENCES memory(id),
    dst INTEGER NOT NULL REFERENCES memory(id),
    kind TEXT NOT NULL,
    probability REAL NOT NULL,
    PRIMARY KEY (src, dst, kind)
);
CREATE TRIGGER IF NOT EXISTS memory_edge_ad AFTER DELETE ON memory BEGIN
    DELETE FROM memory_edge WHERE src = old.id OR dst = old.id;
END;
`

const ftsVersion = "cjk-bigram-1"

func init() {
	sqlite.MustRegisterDeterministicScalarFunction("jev_index_text", 1, func(ctx *sqlite.FunctionContext, args []driver.Value) (driver.Value, error) {
		if len(args) == 0 || args[0] == nil {
			return "", nil
		}
		switch value := args[0].(type) {
		case string:
			return IndexText(value), nil
		case []byte:
			return IndexText(string(value)), nil
		default:
			return "", nil
		}
	})
}

type ExpandResult struct {
	Found bool
	Data  []byte
}

type Row struct {
	ID           int64
	SourceID     string
	Text         string
	SHA256       string
	Probability  sql.NullFloat64
	ModelVersion sql.NullString
	Cwd          string
	SessionID    string
	Role         string
}

type Entry struct {
	Text         string
	Probability  *float64
	ModelVersion string
}

type Store struct {
	path string
	db   *sql.DB
}

func Open(path string) (*Store, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil && filepath.Dir(path) != "." {
		return nil, err
	}
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	if _, err := db.Exec(schema); err != nil {
		db.Close()
		return nil, err
	}
	if err := migrate(db); err != nil {
		db.Close()
		return nil, err
	}
	return &Store{path: path, db: db}, nil
}

func (s *Store) Close() error { return s.db.Close() }

func PointerFor(original []byte) string {
	sum := sha256.Sum256(original)
	return "e:" + hex.EncodeToString(sum[:])
}

func HashText(text string) string {
	sum := sha256.Sum256([]byte(text))
	return hex.EncodeToString(sum[:])
}

func (s *Store) PutElision(original []byte) (string, error) {
	pointer := PointerFor(original)
	sum := sha256.Sum256(original)
	_, err := s.db.Exec(
		`INSERT INTO elision (pointer, original, sha256) VALUES (?, ?, ?)
		 ON CONFLICT(pointer) DO UPDATE SET original = excluded.original, sha256 = excluded.sha256`,
		pointer, original, hex.EncodeToString(sum[:]),
	)
	return pointer, err
}

func (s *Store) Expand(pointer string, page, pageBytes int) (ExpandResult, error) {
	var original []byte
	err := s.db.QueryRow(`SELECT original FROM elision WHERE pointer = ?`, pointer).Scan(&original)
	if err == sql.ErrNoRows {
		return ExpandResult{}, nil
	}
	if err != nil {
		return ExpandResult{}, err
	}
	if page < 0 {
		page = 0
	}
	start := page * pageBytes
	if start >= len(original) {
		return ExpandResult{Found: true}, nil
	}
	end := start + pageBytes
	if end > len(original) {
		end = len(original)
	}
	return ExpandResult{Found: true, Data: append([]byte(nil), original[start:end]...)}, nil
}

func (s *Store) ReplaceSource(collection, sourceID string, entries []Entry) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.Exec(`DELETE FROM memory WHERE collection = ? AND source_id = ?`, collection, sourceID); err != nil {
		return err
	}
	for _, entry := range entries {
		var probability any
		if entry.Probability != nil {
			probability = *entry.Probability
		}
		var version any
		if entry.ModelVersion != "" {
			version = entry.ModelVersion
		}
		if _, err := tx.Exec(
			`INSERT INTO memory (collection, source_id, text, sha256, probability, model_version) VALUES (?, ?, ?, ?, ?, ?)`,
			collection, sourceID, entry.Text, HashText(entry.Text), probability, version,
		); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func (s *Store) InsertMemory(collection, sourceID, text string, probability float64, modelVersion string) error {
	_, err := s.db.Exec(
		`INSERT INTO memory (collection, source_id, text, sha256, probability, model_version) VALUES (?, ?, ?, ?, ?, ?)`,
		collection, sourceID, text, HashText(text), probability, modelVersion,
	)
	return err
}

func (s *Store) CountMemory(collection, sourceID string) (int, error) {
	query := `SELECT COUNT(*) FROM memory`
	var args []any
	var clauses []string
	if collection != "" {
		clauses = append(clauses, "collection = ?")
		args = append(args, collection)
	}
	if sourceID != "" {
		clauses = append(clauses, "source_id = ?")
		args = append(args, sourceID)
	}
	if len(clauses) > 0 {
		query += " WHERE " + strings.Join(clauses, " AND ")
	}
	var n int
	err := s.db.QueryRow(query, args...).Scan(&n)
	return n, err
}

func (s *Store) MemoryRows(collection, sourceID string) ([]Row, error) {
	query := `SELECT source_id, text, sha256, probability, model_version, IFNULL(cwd, ''), IFNULL(session_id, ''), IFNULL(role, ''), id FROM memory WHERE collection = ?`
	args := []any{collection}
	if sourceID != "" {
		query += ` AND source_id = ?`
		args = append(args, sourceID)
	}
	query += ` ORDER BY id`
	rows, err := s.db.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanRows(rows)
}

func (s *Store) Shortlist(collection, query string, limit int) ([]Row, error) {
	match := ftsQuery(query)
	if match == "" {
		return nil, nil
	}
	rows, err := s.db.Query(
		`SELECT m.source_id, m.text, m.sha256, m.probability, m.model_version, IFNULL(m.cwd, ''), IFNULL(m.session_id, ''), IFNULL(m.role, ''), m.id
		 FROM memory_fts JOIN memory AS m ON m.id = memory_fts.rowid
		 WHERE memory_fts MATCH ? AND m.collection = ?
		 ORDER BY bm25(memory_fts) LIMIT ?`,
		match, collection, limit,
	)
	if err != nil {
		return nil, nil
	}
	defer rows.Close()
	return scanRows(rows)
}

func (s *Store) InsertIfNew(collection, sourceID, cwd, sessionID, role, text string) (int64, bool, error) {
	hash := HashText(text)
	var existing int
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM memory WHERE sha256 = ?`, hash).Scan(&existing); err != nil {
		return 0, false, err
	}
	if existing > 0 {
		return 0, false, nil
	}
	result, err := s.db.Exec(
		`INSERT INTO memory (collection, source_id, text, sha256, probability, model_version, cwd, session_id, role) VALUES (?, ?, ?, ?, NULL, NULL, ?, ?, ?)`,
		collection, sourceID, text, hash, cwd, sessionID, role,
	)
	if err != nil {
		return 0, false, err
	}
	id, err := result.LastInsertId()
	if err != nil {
		return 0, false, err
	}
	return id, true, nil
}

func (s *Store) InsertEdge(src, dst int64, kind string, probability float64) error {
	_, err := s.db.Exec(
		`INSERT INTO memory_edge (src, dst, kind, probability) VALUES (?, ?, ?, ?)
		 ON CONFLICT(src, dst, kind) DO UPDATE SET probability = excluded.probability`,
		src, dst, kind, probability,
	)
	return err
}

func (s *Store) Neighborhood(collection string, seedIDs []int64, limit int) ([]Row, error) {
	if len(seedIDs) == 0 || limit <= 0 {
		return nil, nil
	}
	var seed strings.Builder
	args := make([]any, 0, len(seedIDs)+3)
	for i, id := range seedIDs {
		if i > 0 {
			seed.WriteString(", ")
		}
		seed.WriteString("(?, ?)")
		args = append(args, id, i)
	}
	args = append(args, collection, collection, limit)
	query := `
WITH RECURSIVE
seeds(id, ord) AS (VALUES ` + seed.String() + `),
walk(id, depth, score) AS (
    SELECT id, 0, 1.0 FROM seeds
    UNION
    SELECT e.dst, w.depth + 1, e.probability FROM walk AS w
        JOIN memory_edge AS e ON e.src = w.id AND e.kind = 'related'
        JOIN memory AS m ON m.id = e.dst
        WHERE w.depth < 2 AND m.collection = ?
    UNION
    SELECT e.src, w.depth + 1, e.probability FROM walk AS w
        JOIN memory_edge AS e ON e.dst = w.id AND e.kind = 'related'
        JOIN memory AS m ON m.id = e.src
        WHERE w.depth < 2 AND m.collection = ?
),
ranked AS (
    SELECT id, MIN(depth) AS depth, MAX(score) AS score FROM walk GROUP BY id
)
SELECT m.source_id, m.text, m.sha256, m.probability, m.model_version, IFNULL(m.cwd, ''), IFNULL(m.session_id, ''), IFNULL(m.role, ''), m.id, r.depth, r.score
 FROM ranked AS r JOIN memory AS m ON m.id = r.id
 ORDER BY r.depth, CASE WHEN r.depth = 0 THEN r.id END, r.score DESC, r.id
 LIMIT ?`
	rows, err := s.db.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Row
	for rows.Next() {
		var row Row
		var depth int
		var score float64
		if err := rows.Scan(&row.SourceID, &row.Text, &row.SHA256, &row.Probability, &row.ModelVersion, &row.Cwd, &row.SessionID, &row.Role, &row.ID, &depth, &score); err != nil {
			return nil, err
		}
		out = append(out, row)
	}
	return out, rows.Err()
}

func migrate(db *sql.DB) error {
	rows, err := db.Query(`PRAGMA table_info(memory)`)
	if err != nil {
		return err
	}
	defer rows.Close()
	have := map[string]bool{}
	for rows.Next() {
		var cid, notnull, pk int
		var name, ctype string
		var dflt sql.NullString
		if err := rows.Scan(&cid, &name, &ctype, &notnull, &dflt, &pk); err != nil {
			return err
		}
		have[name] = true
	}
	if err := rows.Err(); err != nil {
		return err
	}
	for _, column := range []string{"cwd", "session_id", "role"} {
		if have[column] {
			continue
		}
		if _, err := db.Exec("ALTER TABLE memory ADD COLUMN " + column + " TEXT"); err != nil {
			return err
		}
	}
	return ensureFTS(db)
}

func ensureFTS(db *sql.DB) error {
	if _, err := db.Exec(`CREATE TABLE IF NOT EXISTS jev_meta (key TEXT PRIMARY KEY, value TEXT NOT NULL)`); err != nil {
		return err
	}
	var version string
	_ = db.QueryRow(`SELECT value FROM jev_meta WHERE key = 'fts'`).Scan(&version)
	if version == ftsVersion {
		return nil
	}
	script := `
DROP TRIGGER IF EXISTS memory_ai;
DROP TRIGGER IF EXISTS memory_ad;
DROP TABLE IF EXISTS memory_fts;
CREATE VIRTUAL TABLE memory_fts USING fts5(
    text,
    content='memory',
    content_rowid='id',
    tokenize='unicode61'
);
INSERT INTO memory_fts(rowid, text) SELECT id, jev_index_text(text) FROM memory;
CREATE TRIGGER memory_ai AFTER INSERT ON memory BEGIN
    INSERT INTO memory_fts(rowid, text) VALUES (new.id, jev_index_text(new.text));
END;
CREATE TRIGGER memory_ad AFTER DELETE ON memory BEGIN
    INSERT INTO memory_fts(memory_fts, rowid, text) VALUES ('delete', old.id, jev_index_text(old.text));
END;
`
	if _, err := db.Exec(script); err != nil {
		return err
	}
	_, err := db.Exec(`INSERT INTO jev_meta(key, value) VALUES ('fts', ?) ON CONFLICT(key) DO UPDATE SET value = excluded.value`, ftsVersion)
	return err
}
func (s *Store) Purge() error {
	path := s.path
	if err := s.Close(); err != nil {
		return err
	}
	return os.Remove(path)
}

func scanRows(rows *sql.Rows) ([]Row, error) {
	var out []Row
	for rows.Next() {
		var row Row
		if err := rows.Scan(&row.SourceID, &row.Text, &row.SHA256, &row.Probability, &row.ModelVersion, &row.Cwd, &row.SessionID, &row.Role, &row.ID); err != nil {
			return nil, err
		}
		out = append(out, row)
	}
	return out, rows.Err()
}

func IndexText(text string) string {
	var b strings.Builder
	var han []rune
	var latin []rune
	flushHan := func() {
		if len(han) == 0 {
			return
		}
		if len(han) == 1 {
			writeToken(&b, string(han))
		} else {
			for i := 0; i < len(han)-1; i++ {
				writeToken(&b, string(han[i:i+2]))
			}
		}
		han = han[:0]
	}
	flushLatin := func() {
		if len(latin) == 0 {
			return
		}
		writeToken(&b, string(latin))
		latin = latin[:0]
	}
	for _, char := range text {
		switch {
		case unicode.Is(unicode.Han, char):
			flushLatin()
			han = append(han, char)
		case unicode.IsLetter(char) || unicode.IsDigit(char):
			flushHan()
			latin = append(latin, char)
		default:
			flushHan()
			flushLatin()
		}
	}
	flushHan()
	flushLatin()
	return b.String()
}

func writeToken(b *strings.Builder, token string) {
	if token == "" {
		return
	}
	if b.Len() > 0 {
		b.WriteByte(' ')
	}
	b.WriteString(token)
}

func ftsQuery(text string) string {
	tokens := strings.Fields(IndexText(text))
	if len(tokens) == 0 {
		return ""
	}
	if len(tokens) > 64 {
		tokens = tokens[len(tokens)-64:]
	}
	parts := make([]string, len(tokens))
	for i, token := range tokens {
		parts[i] = fmt.Sprintf("%q", token)
	}
	return strings.Join(parts, " OR ")
}
