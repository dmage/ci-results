package server

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/MakeNowJust/heredoc/v2"
	"github.com/dmage/ci-results/database"
	"github.com/spf13/cobra"
	"k8s.io/klog/v2"
)

type ServerOptions struct {
	db *database.DB
}

func (opts *ServerOptions) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	columns := r.URL.Query().Get("columns")
	if columns == "" {
		columns = "sippytags"
	}

	filter := r.URL.Query().Get("filter")

	periods := r.URL.Query().Get("periods")
	if periods == "" {
		periods = "7,7"
	}

	stats, err := opts.db.BuildStats(columns, filter, periods)
	if err != nil {
		klog.Info(err)
		http.Error(w, "500 internal server error", 500)
		return
	}
	r.Header.Add("Content-Type", "application/json")
	json.NewEncoder(w).Encode(stats)
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
