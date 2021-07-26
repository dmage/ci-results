package server

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"time"

	"github.com/MakeNowJust/heredoc/v2"
	"github.com/dmage/ci-results/database"
	"github.com/spf13/cobra"
	"k8s.io/klog/v2"
)

type ServerOptions struct {
	db *database.DB
}

func (opts *ServerOptions) ServeBuilds(w http.ResponseWriter, r *http.Request) {
	columns := r.URL.Query().Get("columns")
	if columns == "" {
		columns = "sippytags"
	}

	filter := r.URL.Query().Get("filter")

	periods := r.URL.Query().Get("periods")
	if periods == "" {
		periods = "7,7"
	}

	testname := r.URL.Query().Get("testname")

	stats, err := opts.db.BuildStats(columns, filter, periods, testname)
	if err != nil {
		klog.Info(err)
		http.Error(w, "500 internal server error", 500)
		return
	}
	r.Header.Add("Content-Type", "application/json")
	json.NewEncoder(w).Encode(stats)
}

func (opts *ServerOptions) ServeListTests(w http.ResponseWriter, r *http.Request) {
	tests, err := opts.db.ListTests()
	if err != nil {
		klog.Info(err)
		http.Error(w, "500 internal server error", 500)
		return
	}
	r.Header.Add("Content-Type", "application/json")
	json.NewEncoder(w).Encode(tests)
}

func (opts *ServerOptions) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	switch r.URL.Path {
	case "/api/builds":
		opts.ServeBuilds(w, r)
	case "/api/list-tests":
		opts.ServeListTests(w, r)
	default:
		http.NotFound(w, r)
	}
}

func (opts *ServerOptions) Run(ctx context.Context) (err error) {
	db, err := database.OpenDefault()
	if err != nil {
		return fmt.Errorf("unable to open database: %w", err)
	}
	defer func() {
		closeErr := db.Close()
		if err == nil {
			err = closeErr
		}
	}()

	opts.db = db

	go func() {
		time.Sleep(3 * time.Hour)
		os.Exit(0) // Let's get restarted and get new data from TestGrid
	}()

	klog.Info("Starting the API server... http://localhost:8001")
	return http.ListenAndServe(":8001", opts)
}

func NewCmdServer() *cobra.Command {
	opts := &ServerOptions{}

	cmd := &cobra.Command{
		Use:   "server",
		Short: "Serve analytics API for CI data",
		Long: heredoc.Doc(`
			Start an HTTP server with analytical API for CI data.
		`),
		Args: cobra.NoArgs,
		Run: func(cmd *cobra.Command, args []string) {
			err := opts.Run(cmd.Context())
			if err != nil {
				klog.Exit(err)
			}
		},
	}

	return cmd
}
