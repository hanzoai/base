package blob_test

import (
	"context"
	"errors"
	"io"
	"testing"

	"github.com/hanzoai/base/tools/filesystem/blob"
	"github.com/hanzoai/base/tools/filesystem/internal/fileblob"
)

func newBucket(t *testing.T) *blob.Bucket {
	t.Helper()
	drv, err := fileblob.New(t.TempDir(), nil)
	if err != nil {
		t.Fatalf("fileblob.New: %v", err)
	}
	return blob.NewBucket(drv)
}

func write(t *testing.T, b *blob.Bucket, key, content string) {
	t.Helper()
	ctx := context.Background()
	w, err := b.NewWriter(ctx, key, nil)
	if err != nil {
		t.Fatalf("NewWriter(%q): %v", key, err)
	}
	if _, err := w.Write([]byte(content)); err != nil {
		t.Fatalf("Write(%q): %v", key, err)
	}
	if err := w.Close(); err != nil {
		t.Fatalf("writer Close(%q): %v", key, err)
	}
}

func read(t *testing.T, b *blob.Bucket, key string) string {
	t.Helper()
	ctx := context.Background()
	r, err := b.NewReader(ctx, key)
	if err != nil {
		t.Fatalf("NewReader(%q): %v", key, err)
	}
	defer r.Close()
	data, err := io.ReadAll(r)
	if err != nil {
		t.Fatalf("ReadAll(%q): %v", key, err)
	}
	return string(data)
}

func TestWriteReadRoundTrip(t *testing.T) {
	ctx := context.Background()
	b := newBucket(t)
	defer b.Close()

	write(t, b, "dir/hello.txt", "hello blob")

	if got := read(t, b, "dir/hello.txt"); got != "hello blob" {
		t.Fatalf("read back %q, want %q", got, "hello blob")
	}

	exists, err := b.Exists(ctx, "dir/hello.txt")
	if err != nil || !exists {
		t.Fatalf("Exists = %v, %v; want true, nil", exists, err)
	}

	attrs, err := b.Attributes(ctx, "dir/hello.txt")
	if err != nil {
		t.Fatalf("Attributes: %v", err)
	}
	if attrs.Size != int64(len("hello blob")) {
		t.Fatalf("Attributes.Size = %d, want %d", attrs.Size, len("hello blob"))
	}
	if attrs.ContentType == "" {
		t.Fatal("Attributes.ContentType is empty")
	}

	if err := b.Delete(ctx, "dir/hello.txt"); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	exists, err = b.Exists(ctx, "dir/hello.txt")
	if err != nil || exists {
		t.Fatalf("Exists after Delete = %v, %v; want false, nil", exists, err)
	}
	if _, err := b.NewReader(ctx, "dir/hello.txt"); !errors.Is(err, blob.ErrNotFound) {
		t.Fatalf("NewReader after Delete err = %v, want ErrNotFound", err)
	}
}

func TestRangeReader(t *testing.T) {
	b := newBucket(t)
	defer b.Close()

	write(t, b, "range.txt", "hello blob")

	r, err := b.NewRangeReader(context.Background(), "range.txt", 2, 3)
	if err != nil {
		t.Fatalf("NewRangeReader: %v", err)
	}
	defer r.Close()
	data, err := io.ReadAll(r)
	if err != nil {
		t.Fatalf("ReadAll: %v", err)
	}
	if string(data) != "llo" {
		t.Fatalf("range read %q, want %q", string(data), "llo")
	}
}

func listKeys(t *testing.T, b *blob.Bucket, opts *blob.ListOptions) []*blob.ListObject {
	t.Helper()
	var out []*blob.ListObject
	it := b.List(opts)
	for {
		obj, err := it.Next(context.Background())
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatalf("List Next: %v", err)
		}
		out = append(out, obj)
	}
	return out
}

func TestListFlatAndDelimiter(t *testing.T) {
	b := newBucket(t)
	defer b.Close()

	write(t, b, "a/1.txt", "1")
	write(t, b, "a/2.txt", "2")
	write(t, b, "b.txt", "b")

	flat := listKeys(t, b, nil)
	if len(flat) != 3 {
		t.Fatalf("flat list returned %d objects, want 3", len(flat))
	}
	wantOrder := []string{"a/1.txt", "a/2.txt", "b.txt"}
	for i, want := range wantOrder {
		if flat[i].Key != want {
			t.Fatalf("flat[%d].Key = %q, want %q", i, flat[i].Key, want)
		}
		if flat[i].IsDir {
			t.Fatalf("flat[%d] (%q) marked IsDir", i, flat[i].Key)
		}
	}

	hier := listKeys(t, b, &blob.ListOptions{Delimiter: "/"})
	if len(hier) != 2 {
		t.Fatalf("delimited list returned %d objects, want 2", len(hier))
	}
	if hier[0].Key != "a/" || !hier[0].IsDir {
		t.Fatalf("hier[0] = %q (IsDir=%v), want \"a/\" dir", hier[0].Key, hier[0].IsDir)
	}
	if hier[1].Key != "b.txt" || hier[1].IsDir {
		t.Fatalf("hier[1] = %q (IsDir=%v), want \"b.txt\" file", hier[1].Key, hier[1].IsDir)
	}
}

func TestListPagination(t *testing.T) {
	ctx := context.Background()
	b := newBucket(t)
	defer b.Close()

	write(t, b, "a/1.txt", "1")
	write(t, b, "a/2.txt", "2")
	write(t, b, "b.txt", "b")

	page1, token, err := b.ListPage(ctx, blob.FirstPageToken, 2, nil)
	if err != nil {
		t.Fatalf("ListPage page 1: %v", err)
	}
	if len(page1) != 2 {
		t.Fatalf("page 1 has %d objects, want 2", len(page1))
	}
	if len(token) == 0 {
		t.Fatal("page 1 returned empty next token, want non-empty")
	}

	page2, token, err := b.ListPage(ctx, token, 2, nil)
	if err != nil {
		t.Fatalf("ListPage page 2: %v", err)
	}
	if len(page2) != 1 {
		t.Fatalf("page 2 has %d objects, want 1", len(page2))
	}
	if len(token) != 0 {
		t.Fatalf("page 2 returned non-empty next token %q, want empty", token)
	}

	seen := map[string]bool{}
	for _, o := range append(page1, page2...) {
		seen[o.Key] = true
	}
	for _, want := range []string{"a/1.txt", "a/2.txt", "b.txt"} {
		if !seen[want] {
			t.Fatalf("paginated list missing %q", want)
		}
	}
}

func TestKeyEscapingRoundTrip(t *testing.T) {
	b := newBucket(t)
	defer b.Close()

	key := "weird dir/ünïcode key.txt"
	write(t, b, key, "escaped")

	if got := read(t, b, key); got != "escaped" {
		t.Fatalf("read back %q, want %q", got, "escaped")
	}

	flat := listKeys(t, b, nil)
	if len(flat) != 1 || flat[0].Key != key {
		t.Fatalf("listed keys = %v, want exactly %q", flat, key)
	}
}

func TestClosedBucket(t *testing.T) {
	ctx := context.Background()
	b := newBucket(t)

	write(t, b, "x.txt", "x")

	if err := b.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	if _, err := b.NewReader(ctx, "x.txt"); !errors.Is(err, blob.ErrClosed) {
		t.Fatalf("NewReader after Close err = %v, want ErrClosed", err)
	}
	if _, err := b.NewWriter(ctx, "y.txt", nil); !errors.Is(err, blob.ErrClosed) {
		t.Fatalf("NewWriter after Close err = %v, want ErrClosed", err)
	}
	if _, err := b.Attributes(ctx, "x.txt"); !errors.Is(err, blob.ErrClosed) {
		t.Fatalf("Attributes after Close err = %v, want ErrClosed", err)
	}
	if _, _, err := b.ListPage(ctx, blob.FirstPageToken, 1, nil); !errors.Is(err, blob.ErrClosed) {
		t.Fatalf("ListPage after Close err = %v, want ErrClosed", err)
	}
	if _, err := b.List(nil).Next(ctx); !errors.Is(err, blob.ErrClosed) {
		t.Fatalf("List Next after Close err = %v, want ErrClosed", err)
	}
	if err := b.Delete(ctx, "x.txt"); !errors.Is(err, blob.ErrClosed) {
		t.Fatalf("Delete after Close err = %v, want ErrClosed", err)
	}
}
