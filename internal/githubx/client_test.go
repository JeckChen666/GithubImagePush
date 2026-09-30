package githubx

import (
	"bytes"
	"context"
	"errors"
	"testing"

	"githubimagepush/internal/testutil"
)

func newTestClient(t *testing.T) (*Client, *testutil.MockGitHub) {
	t.Helper()
	mock := testutil.NewMockGitHub("goodtoken")
	t.Cleanup(mock.Close)
	c := New(mock.URL(), "goodtoken", "someone/images", "main")
	return c, mock
}

func TestUploadCreate(t *testing.T) {
	c, _ := newTestClient(t)
	content := []byte("hello png bytes")
	info, err := c.Upload(context.Background(), "img/2026/a.png", content, "msg")
	if err != nil {
		t.Fatal(err)
	}
	if info.Path != "img/2026/a.png" || info.Size != int64(len(content)) {
		t.Errorf("info = %+v", info)
	}
	if info.SHA == "" {
		t.Errorf("sha 不应为空")
	}
}

func TestUploadExists(t *testing.T) {
	c, _ := newTestClient(t)
	_, first := c.Upload(context.Background(), "img/a.png", []byte("v1"), "m")
	if first != nil {
		t.Fatal(first)
	}
	_, err := c.Upload(context.Background(), "img/a.png", []byte("v2"), "m")
	if !errors.Is(err, ErrExists) {
		t.Fatalf("期望 ErrExists，实际 %v", err)
	}
}

func TestUploadReplace(t *testing.T) {
	c, _ := newTestClient(t)
	if _, err := c.Upload(context.Background(), "img/a.png", []byte("v1"), "m"); err != nil {
		t.Fatal(err)
	}
	info, err := c.UploadReplace(context.Background(), "img/a.png", []byte("v2"), "m")
	if err != nil {
		t.Fatal(err)
	}
	got, err := c.Get(context.Background(), "img/a.png")
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got.DecodedContent(), []byte("v2")) {
		t.Errorf("覆盖后内容 = %q, want %q", got.DecodedContent(), "v2")
	}
	if got.SHA != info.SHA {
		t.Errorf("sha 不一致")
	}
}

func TestGetAndList(t *testing.T) {
	c, _ := newTestClient(t)
	_, err := c.Get(context.Background(), "nope.png")
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("期望 ErrNotFound，实际 %v", err)
	}
	_ = mustUpload(t, c, "img/2026/a.png")
	_ = mustUpload(t, c, "img/2026/b.png")

	entries, err := c.List(context.Background(), "img/2026")
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 2 {
		t.Fatalf("目录条目数 = %d, want 2", len(entries))
	}
	if entries[0].Type != "file" {
		t.Errorf("条目类型 = %q", entries[0].Type)
	}

	// 不存在的目录
	if _, err := c.List(context.Background(), "no/such/dir"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("期望 ErrNotFound，实际 %v", err)
	}
}

func TestDelete(t *testing.T) {
	c, _ := newTestClient(t)
	_ = mustUpload(t, c, "img/a.png")

	info, err := c.Get(context.Background(), "img/a.png")
	if err != nil {
		t.Fatal(err)
	}
	if err := c.Delete(context.Background(), "img/a.png", info.SHA, "del"); err != nil {
		t.Fatal(err)
	}
	if _, err := c.Get(context.Background(), "img/a.png"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("删除后应 NotFound，实际 %v", err)
	}
	if err := c.Delete(context.Background(), "img/a.png", "bad-sha", "del"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("删除不存在文件应 NotFound，实际 %v", err)
	}
}

func TestUnauthorizedToken(t *testing.T) {
	mock := testutil.NewMockGitHub("goodtoken")
	t.Cleanup(mock.Close)
	c := New(mock.URL(), "badtoken", "someone/images", "main")
	_, err := c.Upload(context.Background(), "img/a.png", []byte("x"), "m")
	if !IsUnauthorized(err) {
		t.Fatalf("期望鉴权失败错误，实际 %v", err)
	}
	var ae *APIError
	if !errors.As(err, &ae) || ae.Status != 401 {
		t.Fatalf("错误应为 401 APIError，实际 %v", err)
	}
}

func TestBlobContent(t *testing.T) {
	c, mock := newTestClient(t)
	mock.Seed("img/big.png", []byte("blobby"))
	info, err := c.Get(context.Background(), "img/big.png")
	if err != nil {
		t.Fatal(err)
	}
	content, err := c.BlobContent(context.Background(), info.SHA)
	if err != nil {
		t.Fatal(err)
	}
	if string(content) != "blobby" {
		t.Errorf("blob 内容 = %q", content)
	}
}

func mustUpload(t *testing.T, c *Client, p string) *FileInfo {
	t.Helper()
	info, err := c.Upload(context.Background(), p, []byte("content of "+p), "m")
	if err != nil {
		t.Fatal(err)
	}
	return info
}
