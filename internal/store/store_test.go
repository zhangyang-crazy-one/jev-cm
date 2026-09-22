package store

import (
	"database/sql"
	"path/filepath"
	"testing"

	_ "modernc.org/sqlite"
)

func TestIndexTextSplitsScriptAndHanPairs(t *testing.T) {
	if got := IndexText("jev插件开发"); got != "jev 插件 件开 开发" {
		t.Fatalf("%q", got)
	}
	if got := IndexText("正在开发插件"); got != "正在 在开 开发 发插 插件" {
		t.Fatalf("%q", got)
	}
	if got := IndexText("refund policy"); got != "refund policy" {
		t.Fatalf("%q", got)
	}
	if ftsQuery("jev插件开发") != `"jev" OR "插件" OR "件开" OR "开发"` {
		t.Fatalf("%q", ftsQuery("jev插件开发"))
	}
}

func TestShortlistFindsHanInsideALongerRun(t *testing.T) {
	opened := openTestStore(t)
	id, inserted, err := opened.InsertIfNew("conversation", "s1", "/proj", "s1", "user", "之前就在进行 jev 插件的开发，匹配还没切开汉字")
	if err != nil || !inserted || id == 0 {
		t.Fatal(err, inserted, id)
	}
	if err != nil || !inserted {
		t.Fatal(err, inserted)
	}
	rows, err := opened.Shortlist("conversation", "jev插件开发", 20)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 || rows[0].Text == "" {
		t.Fatalf("%#v", rows)
	}
}

func TestOpenRebuildsOldFullTextIndex(t *testing.T) {
	path := filepath.Join(t.TempDir(), "old.sqlite")
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	schema := `
CREATE TABLE memory (
    id INTEGER PRIMARY KEY,
    collection TEXT NOT NULL,
    source_id TEXT NOT NULL,
    text TEXT NOT NULL,
    sha256 TEXT NOT NULL,
    probability REAL,
    model_version TEXT
);
CREATE VIRTUAL TABLE memory_fts USING fts5(text, content='memory', content_rowid='id', tokenize='unicode61');
CREATE TRIGGER memory_ai AFTER INSERT ON memory BEGIN
    INSERT INTO memory_fts(rowid, text) VALUES (new.id, new.text);
END;
`
	if _, err := db.Exec(schema); err != nil {
		t.Fatal(err)
	}
	body := "正在开发这个jev插件"
	if _, err := db.Exec(`INSERT INTO memory (collection, source_id, text, sha256) VALUES ('conversation', 's1', ?, 'abc')`, body); err != nil {
		t.Fatal(err)
	}
	db.Close()
	opened, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer opened.Close()
	rows, err := opened.Shortlist("conversation", "插件开发", 20)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 || rows[0].Text != body {
		t.Fatalf("%#v", rows)
	}
	again, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer again.Close()
	rows, err = again.Shortlist("conversation", "插件开发", 20)
	if err != nil || len(rows) != 1 {
		t.Fatalf("%v %#v", err, rows)
	}
}

func TestNeighborhoodWalksEdgesBothWays(t *testing.T) {
	opened := openTestStore(t)
	seedID, _, err := opened.InsertIfNew("conversation", "s1", "/proj", "s1", "user", "alpha refund policy notes")
	if err != nil {
		t.Fatal(err)
	}
	neighborID, _, err := opened.InsertIfNew("conversation", "s1", "/proj", "s1", "assistant", "beta shipping dock schedule")
	if err != nil {
		t.Fatal(err)
	}
	if err := opened.InsertEdge(seedID, neighborID, "related", 0.9); err != nil {
		t.Fatal(err)
	}
	rows, err := opened.Neighborhood("conversation", []int64{seedID}, 20)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 2 || rows[0].ID != seedID || rows[1].ID != neighborID {
		t.Fatalf("%#v", rows)
	}
	back, err := opened.Neighborhood("conversation", []int64{neighborID}, 20)
	if err != nil {
		t.Fatal(err)
	}
	if len(back) != 2 || back[0].ID != neighborID || back[1].ID != seedID {
		t.Fatalf("%#v", back)
	}
	other, _, err := opened.InsertIfNew("durable", "s2", "/proj", "s2", "user", "alpha unrelated durable entry")
	if err != nil {
		t.Fatal(err)
	}
	if err := opened.InsertEdge(neighborID, other, "related", 0.8); err != nil {
		t.Fatal(err)
	}
	keptInside, err := opened.Neighborhood("conversation", []int64{seedID}, 20)
	if err != nil {
		t.Fatal(err)
	}
	if len(keptInside) != 2 {
		t.Fatalf("cross-collection leak: %#v", keptInside)
	}
}

func TestNeighborhoodWithoutEdgesAndEmptySeeds(t *testing.T) {
	opened := openTestStore(t)
	lonelyID, _, err := opened.InsertIfNew("conversation", "s1", "/proj", "s1", "user", "lonely alpha entry")
	if err != nil {
		t.Fatal(err)
	}
	rows, err := opened.Neighborhood("conversation", []int64{lonelyID}, 20)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 || rows[0].ID != lonelyID {
		t.Fatalf("%#v", rows)
	}
	if rows, err := opened.Neighborhood("conversation", nil, 20); err != nil || rows != nil {
		t.Fatalf("%#v %v", rows, err)
	}
}

func openTestStore(t *testing.T) *Store {
	t.Helper()
	opened, err := Open(filepath.Join(t.TempDir(), "jev.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { opened.Close() })
	return opened
}
