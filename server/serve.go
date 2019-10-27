package server

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/sirupsen/logrus"
	tomb "gopkg.in/tomb.v2"

	"github.com/suhailpatel/seastat/flags"
	"github.com/suhailpatel/seastat/jolokia"
)

// Run takes in the Jolokia client and some options and does everything needed
// to start scraping and serving metrics
func Run(client jolokia.Client, interval time.Duration, port int) {
	// Parent context to track all our child goroutines
	ctx, cancel := context.WithCancel(context.Background())

	addr := fmt.Sprintf(":%d", port)
	logrus.Infof("👂 Listening on %s", addr)

	srv := &http.Server{Addr: addr}
	http.Handle("/metrics", promhttp.Handler())
	http.HandleFunc("/healthz", handleHealthz(client))
	http.HandleFunc("/", handleRoot())

	// This tomb will take care of all our goroutines such as the scraper and
	// the webserver. If something unexpected happens or we need to gracefully
	// terminate, it'll keep track of everything pending
	t := tomb.Tomb{}

	// Start up our webserver
	t.Go(func() error {
		// Set up our server for graceful shutdown when our context terminates
		t.Go(func() error {
			<-ctx.Done()
			srv.Shutdown(ctx)
			return nil
		})

		if err := srv.ListenAndServe(); err != http.ErrServerClosed {
			logrus.Errorf("error whilst serving: %v", err)
			t.Kill(fmt.Errorf("error whilst serving: %v", err))
		}
		logrus.Infof("😴 Server has shut down")
		return nil
	})

	// Handle signal termination by cancelling our context which should
	sigs := make(chan os.Signal, 1)
	signal.Notify(sigs, syscall.SIGINT, syscall.SIGTERM)
	select {
	case sig := <-sigs:
		logrus.Infof("🏁 Received OS signal %v, shutting down", sig)
	case <-t.Dying():
		logrus.Infof("⚰️ Tomb is dying, shutting down")
	}
	cancel()

	// Wait a maximum of 10 seconds for everything to cleanly shut down
	select {
	case <-t.Dead():
		logrus.Infof("👋 Goodbye!")
	case <-time.After(10 * time.Second):
		logrus.Errorf("🔴 Did not gracefully terminate in time, force exiting")
		os.Exit(128)
	}
}

func handleRoot() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		fmt.Fprintf(w, "🌊 Seastat Cassandra Exporter %v (Commit: %v)", flags.Version, flags.GitCommitHash)
	}
}

func handleHealthz(client jolokia.Client) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		jolokiaVersion, err := client.Version()
		if err != nil {
			w.WriteHeader(http.StatusServiceUnavailable)
			v, _ := json.Marshal(map[string]string{"error": fmt.Sprintf("%v", err)}) // not much we can do if this errors
			w.Write(v)
			return
		}

		w.WriteHeader(http.StatusOK)
		v, _ := json.Marshal(map[string]string{"jolokia": jolokiaVersion, "seastat": flags.Version})
		w.Write(v)
	}
}
