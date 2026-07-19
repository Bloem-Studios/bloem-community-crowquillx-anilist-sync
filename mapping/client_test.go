package mapping

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestClientValidatesSchemaMajor(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"$meta":{"schema_version":"4.0.0"}}`))
	}))
	defer server.Close()
	client := NewClient(server.Client())
	client.url = server.URL
	if _, err := client.Dataset(context.Background()); err == nil {
		t.Fatal("expected unsupported schema to fail")
	}
}

func TestClientKeepsLastKnownGoodDataset(t *testing.T) {
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if calls == 1 {
			_, _ = w.Write([]byte(`{"$meta":{"schema_version":"3.0.3"},"tmdb_movie:1":{"anilist:2":{"1":"1"}}}`))
			return
		}
		http.Error(w, "offline", http.StatusServiceUnavailable)
	}))
	defer server.Close()
	client := NewClient(server.Client())
	client.url = server.URL
	client.maxAge = time.Nanosecond
	first, err := client.Dataset(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	time.Sleep(time.Millisecond)
	second, err := client.Dataset(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if first["tmdb_movie:1"] == nil || second["tmdb_movie:1"] == nil {
		t.Fatalf("last known good mapping was not retained")
	}
}
