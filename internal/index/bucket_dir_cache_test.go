package index

import (
	"errors"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/vshulcz/deja-vu/internal/query"
)

// bucketDirStore is a Claude store under a temp dir and the index beside it.
// add writes one single-turn session and indexes it.
func bucketDirStore(t *testing.T) (dir string, add func(id, text string)) {
	t.Helper()
	tmp := t.TempDir()
	root := filepath.Join(tmp, "claude")
	t.Setenv("DEJA_CLAUDE_ROOT", root)
	dir = filepath.Join(tmp, "index.db")
	t.Setenv("DEJA_INDEX_DIR", dir)
	proj := filepath.Join(root, "-tmp-orchard")
	if err := os.MkdirAll(proj, 0o755); err != nil {
		t.Fatal(err)
	}
	add = func(id, text string) {
		t.Helper()
		line := `{"type":"user","sessionId":"` + id + `","timestamp":"2026-01-02T03:04:05Z","message":{"role":"user","content":"` + text + `"}}` + "\n"
		if err := os.WriteFile(filepath.Join(proj, id+".jsonl"), []byte(line), 0o644); err != nil {
			t.Fatal(err)
		}
		if err := Ensure(dir, "", true, nil); err != nil {
			t.Fatal(err)
		}
	}
	return dir, add
}

func keyOf(t *testing.T, word string) string {
	t.Helper()
	keys := queryKeys(word)
	if len(keys) != 1 {
		t.Fatalf("%q has keys %q, want one", word, keys)
	}
	return keys[0]
}

func sessionsHolding(t *testing.T, dir, word string) int {
	t.Helper()
	posts, err := postingsFor(dir, keyOf(t, word))
	if err != nil {
		t.Fatal(err)
	}
	seen := map[uint32]bool{}
	for _, p := range posts {
		seen[p.Sid] = true
	}
	return len(seen)
}

// cachedStale reports a cached directory of dir whose file on disk is no
// longer the one it was read from.
func cachedStale(dir string) string {
	bucketDirCache.mu.Lock()
	defer bucketDirCache.mu.Unlock()
	if bucketDirCache.dir != dir {
		return ""
	}
	for p, d := range bucketDirCache.byPath {
		fi, err := os.Stat(p)
		if err != nil || !sameFileVersion(d.stat, fi) {
			return p
		}
	}
	return ""
}

// cachedDirs is the directories cached for dir.
func cachedDirs(dir string) map[*cachedBucketDir]bool {
	bucketDirCache.mu.Lock()
	defer bucketDirCache.mu.Unlock()
	out := map[*cachedBucketDir]bool{}
	if bucketDirCache.dir == dir {
		for _, d := range bucketDirCache.byPath {
			out[d] = true
		}
	}
	return out
}

// The bucket directories a query read are kept for the next one. A publish
// is another generation of the index: the next query reads the new
// postings, and no directory of the files it replaced is kept.
func TestBucketDirsFollowAPublish(t *testing.T) {
	dir, add := bucketDirStore(t)
	add("s1", "the pear tree needs grafting before the thaw")
	if n := sessionsHolding(t, dir, "grafting"); n != 1 {
		t.Fatalf("first build: grafting in %d sessions, want 1", n)
	}
	before := bucketDirReads.Load()
	if n := sessionsHolding(t, dir, "grafting"); n != 1 {
		t.Fatalf("second read: grafting in %d sessions, want 1", n)
	}
	if n := bucketDirReads.Load() - before; n != 0 {
		t.Fatalf("a second query read %d bucket directories off disk, want 0", n)
	}

	kept := cachedDirs(dir)
	if len(kept) == 0 {
		t.Fatal("control: nothing cached after two queries")
	}
	add("s2", "grafting wax and a pruning saw for the pear tree")
	for d := range cachedDirs(dir) {
		if kept[d] {
			t.Fatal("a directory read before the publish is still cached after it")
		}
	}
	add("s3", "the quince took its grafting scion in march")
	if n := sessionsHolding(t, dir, "grafting"); n != 3 {
		t.Fatalf("after two publishes grafting is in %d sessions, want 3", n)
	}
	if n := sessionsHolding(t, dir, "quince"); n != 1 {
		t.Fatalf("after a publish the new word quince is in %d sessions, want 1", n)
	}
	if p := cachedStale(dir); p != "" {
		t.Fatalf("after a publish the cache still holds the directory of a file it replaced: %s", p)
	}
	ss, err := Search(dir, query.Options{Query: "grafting", All: true})
	if err != nil {
		t.Fatal(err)
	}
	if len(ss) != 3 {
		t.Fatalf("search after a publish found %d sessions, want 3", len(ss))
	}
}

// A bucket file replaced under an unchanged manifest, the window between a
// writer renaming its buckets in and committing the manifest, or a file
// damaged in place, is read as it is now, not through the directory of the
// file it replaced.
func TestBucketDirsFollowAFileReplacedUnderTheManifest(t *testing.T) {
	dir, add := bucketDirStore(t)
	add("s1", "the medlar tree fruits late in november")
	key := keyOf(t, "medlar")
	if n := sessionsHolding(t, dir, "medlar"); n != 1 {
		t.Fatalf("first build: medlar in %d sessions, want 1", n)
	}
	p, ok := bucketPath(dir, key)
	if !ok {
		t.Fatal("no bucket for medlar")
	}
	// A word of the same bucket that no session holds.
	other := ""
	for _, w := range []string{"mealybug", "meadow", "medick", "mezereon", "melon", "medicinal"} {
		if k := keyOf(t, w); bucket(k) == bucket(key) {
			other = w
			break
		}
	}
	if other == "" {
		t.Fatal("no candidate word shares medlar's bucket")
	}
	if n := sessionsHolding(t, dir, other); n != 0 {
		t.Fatalf("control: %s in %d sessions before the rewrite", other, n)
	}
	data, err := readBucketFile(p)
	if err != nil {
		t.Fatal(err)
	}
	data[keyOf(t, other)] = data[key]
	if err := writeBucket(p, data); err != nil {
		t.Fatal(err)
	}
	if n := sessionsHolding(t, dir, other); n != 1 {
		t.Fatalf("a bucket rewritten under the same manifest still read through the old directory: %s in %d sessions, want 1", other, n)
	}

	// Damaged in place: the error that sends the reader to rebuild, not an
	// answer from memory.
	if err := os.WriteFile(p, []byte("not a bucket at all"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := postingsFor(dir, key); !errors.Is(err, errCorruptIndex) {
		t.Fatalf("a damaged bucket read as %v, want a corrupt-index error", err)
	}
}

// Concurrent recalls share the cached directories while the index is
// written under them. Run with -race.
func TestBucketDirsConcurrentReaders(t *testing.T) {
	dir, add := bucketDirStore(t)
	add("s1", "the walnut drops its leaves first")
	var wg sync.WaitGroup
	stop := make(chan struct{})
	for range 8 {
		wg.Go(func() {
			for {
				select {
				case <-stop:
					return
				default:
				}
				_, _ = postingsFor(dir, "twalnut")
				_, _ = SearchDetailed(dir, query.Options{Query: "walnut leaves", All: true})
			}
		})
	}
	add("s2", "a walnut sapling by the gate")
	add("s3", "walnut husks stain the hands")
	close(stop)
	wg.Wait()
	if n := sessionsHolding(t, dir, "walnut"); n != 3 {
		t.Fatalf("after concurrent reads walnut is in %d sessions, want 3", n)
	}
}
