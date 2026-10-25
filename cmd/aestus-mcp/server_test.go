package main

import (
	"context"
	"encoding/json"
	"net"
	"net/http"
	"path/filepath"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/yurika0211/aestus/memory"
)

func TestRememberRecallGetForgetRoundTrip(t *testing.T) {
	session := startSession(t)

	remembered := callTool[rememberOutput](t, session, "aestus_remember", map[string]any{
		"content":     "Project directory is /workspace/example",
		"category":    "project",
		"tier":        "long",
		"importance":  0.9,
		"mode":        "upsert_state",
		"state_key":   "project.directory",
		"state_value": "/workspace/example",
		"tags":        []string{"project"},
	})
	if !remembered.Created || remembered.ID == "" {
		t.Fatalf("remember = %#v", remembered)
	}

	recalled := callTool[recallOutput](t, session, "aestus_recall", map[string]any{
		"query": "project directory",
		"limit": 5,
	})
	if len(recalled.Notes) == 0 || recalled.Notes[0].ID != remembered.ID {
		t.Fatalf("recall = %#v, want id %s", recalled, remembered.ID)
	}
	if recalled.Notes[0].Excerpt == "" {
		t.Fatal("recall excerpt is empty")
	}

	got := callTool[getOutput](t, session, "aestus_get", map[string]any{"id": remembered.ID})
	if got.Note.Content != "Project directory is /workspace/example" {
		t.Fatalf("get content = %q", got.Note.Content)
	}

	note, err := session.ReadResource(context.Background(), &mcp.ReadResourceParams{URI: rememberedNoteURI(remembered.ID)})
	if err != nil {
		t.Fatal(err)
	}
	if len(note.Contents) == 0 || !strings.Contains(note.Contents[0].Text, "Project directory is /workspace/example") {
		t.Fatalf("resource = %#v", note.Contents)
	}

	stats := callTool[statsOutput](t, session, "aestus_stats", map[string]any{})
	if stats.Long < 1 || stats.Total < 1 {
		t.Fatalf("stats = %#v", stats)
	}

	forgotten := callTool[forgetOutput](t, session, "aestus_forget", map[string]any{"id": remembered.ID})
	if !forgotten.Deleted || forgotten.ID != remembered.ID {
		t.Fatalf("forget = %#v", forgotten)
	}
	if _, err := session.CallTool(context.Background(), &mcp.CallToolParams{
		Name:      "aestus_get",
		Arguments: map[string]any{"id": remembered.ID},
	}); err != nil {
		t.Fatal(err)
	}
	missing := callToolRaw(t, session, "aestus_get", map[string]any{"id": remembered.ID})
	if !missing.IsError {
		t.Fatalf("deleted get = %#v", missing)
	}
}

func TestCancelledRememberDoesNotWrite(t *testing.T) {
	dir := t.TempDir()
	store, err := memory.NewStore(filepath.Join(dir, "vault"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	h := &handler{store: store}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, _, err = h.remember(ctx, nil, rememberInput{Content: "should not persist", Category: "fact"})
	if err == nil {
		t.Fatal("cancelled remember returned nil error")
	}
	if store.Count() != 0 {
		t.Fatalf("vault count = %d, want 0", store.Count())
	}
}

func TestStreamableHTTPStats(t *testing.T) {
	store, err := memory.NewStore(filepath.Join(t.TempDir(), "vault"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	if _, err := store.Remember(context.Background(), memory.RememberRequest{
		Content: "HTTP probe fact", Category: "fact", Tier: memory.TierLong, Importance: 0.5,
	}); err != nil {
		t.Fatal(err)
	}

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	server := NewServer(store)
	handler := mcp.NewStreamableHTTPHandler(func(*http.Request) *mcp.Server { return server }, &mcp.StreamableHTTPOptions{Stateless: true, JSONResponse: true})
	httpServer := &http.Server{Handler: handler}
	go func() { _ = httpServer.Serve(listener) }()
	t.Cleanup(func() { _ = httpServer.Shutdown(context.Background()) })

	ctx := context.Background()
	client := mcp.NewClient(&mcp.Implementation{Name: "aestus-http-test", Version: "test"}, nil)
	session, err := client.Connect(ctx, &mcp.StreamableClientTransport{Endpoint: "http://" + listener.Addr().String()}, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = session.Close() })

	stats := callTool[statsOutput](t, session, "aestus_stats", map[string]any{})
	if stats.Total < 1 {
		t.Fatalf("http stats = %#v", stats)
	}
}

func TestRecallRequiresQuery(t *testing.T) {
	session := startSession(t)
	result := callToolRaw(t, session, "aestus_recall", map[string]any{"query": "   "})
	if !result.IsError {
		t.Fatalf("blank recall = %#v", result)
	}
}

func startSession(t *testing.T) *mcp.ClientSession {
	t.Helper()
	store, err := memory.NewStore(filepath.Join(t.TempDir(), "vault"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })

	ctx := context.Background()
	clientTransport, serverTransport := mcp.NewInMemoryTransports()
	serverSession, err := NewServer(store).Connect(ctx, serverTransport, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = serverSession.Close() })

	client := mcp.NewClient(&mcp.Implementation{Name: "aestus-mcp-test", Version: "test"}, nil)
	session, err := client.Connect(ctx, clientTransport, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = session.Close() })
	return session
}

func callTool[T any](t *testing.T, session *mcp.ClientSession, name string, args map[string]any) T {
	t.Helper()
	result := callToolRaw(t, session, name, args)
	if result.IsError {
		t.Fatalf("%s error: %#v", name, result.Content)
	}
	var out T
	raw, err := json.Marshal(result.StructuredContent)
	if err != nil {
		t.Fatalf("%s structured content: %v (%T)", name, err, result.StructuredContent)
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatalf("%s decode %s: %v", name, raw, err)
	}
	return out
}

func callToolRaw(t *testing.T, session *mcp.ClientSession, name string, args map[string]any) *mcp.CallToolResult {
	t.Helper()
	result, err := session.CallTool(context.Background(), &mcp.CallToolParams{
		Name:      name,
		Arguments: args,
	})
	if err != nil {
		t.Fatalf("%s: %v", name, err)
	}
	return result
}

func rememberedNoteURI(id string) string {
	return noteURIPrefix + id
}
