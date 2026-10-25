// Command aestus-mcp exposes one Aestus vault as an MCP memory server.
//
// The process operator chooses the vault. Tool arguments cannot select
// another directory, a caller namespace, or an authorization filter.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/yurika0211/aestus/memory"
)

func main() {
	log.SetFlags(0)
	log.SetPrefix("aestus-mcp: ")

	vault := flag.String("vault", "", "Aestus vault directory (or AESTUS_VAULT)")
	httpAddr := flag.String("http", "", "streamable HTTP listen address; empty uses stdio")
	flag.Parse()

	dir := strings.TrimSpace(*vault)
	if dir == "" {
		dir = strings.TrimSpace(os.Getenv("AESTUS_VAULT"))
	}
	if dir == "" {
		fmt.Fprintln(os.Stderr, "aestus-mcp: set --vault or AESTUS_VAULT to the vault directory")
		os.Exit(2)
	}

	store, err := memory.NewStore(dir)
	if err != nil {
		log.Fatalf("open vault: %v", err)
	}
	defer func() {
		if err := store.Close(); err != nil {
			log.Printf("close vault: %v", err)
		}
	}()

	server := NewServer(store)
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	if strings.TrimSpace(*httpAddr) == "" {
		if err := server.Run(ctx, &mcp.StdioTransport{}); err != nil && !errors.Is(err, context.Canceled) {
			log.Fatalf("stdio server: %v", err)
		}
		return
	}
	if err := serveHTTP(ctx, server, strings.TrimSpace(*httpAddr)); err != nil && !errors.Is(err, http.ErrServerClosed) {
		log.Fatalf("http server: %v", err)
	}
}

func serveHTTP(ctx context.Context, server *mcp.Server, addr string) error {
	listener, err := net.Listen("tcp", addr)
	if err != nil {
		return err
	}
	handler := mcp.NewStreamableHTTPHandler(func(*http.Request) *mcp.Server {
		return server
	}, &mcp.StreamableHTTPOptions{Stateless: true, JSONResponse: true})
	httpServer := &http.Server{Handler: handler}
	errCh := make(chan error, 1)
	go func() {
		log.Printf("streamable HTTP listening on %s", listener.Addr())
		errCh <- httpServer.Serve(listener)
	}()
	select {
	case <-ctx.Done():
		return httpServer.Shutdown(context.Background())
	case err := <-errCh:
		return err
	}
}
