// Command astronomyCalculator serves a web front end that casts an astrology
// birth chart — the planets, the Ascendant and Descendant, the twelve houses,
// and a chart wheel — from a date, a time and a place.
//
// Everything it needs is compiled into the binary: the VSOP87D planetary
// theory, the IANA time-zone database and a gazetteer of about seventy
// thousand places. It makes no network calls unless started with -online, and
// even then only when the user asks for an online place search.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"runtime"
	"syscall"
	"time"

	_ "time/tzdata" // ship the full historical zone database, DST rules included

	"astronomyCalculator/internal/web"
)

func main() {
	addr := flag.String("addr", "localhost:8080", "address to listen on")
	online := flag.Bool("online", false,
		"allow the opt-in online place search; off by default so nothing leaves this machine")
	open := flag.Bool("open", false, "open the application in a browser once it is listening")
	flag.Parse()

	logger := log.New(os.Stderr, "", log.LstdFlags)

	if err := run(*addr, *online, *open, logger); err != nil {
		logger.Fatalf("astronomyCalculator: %v", err)
	}
}

func run(addr string, online, open bool, logger *log.Logger) error {
	server, err := web.New(online, logger)
	if err != nil {
		return err
	}

	srv := &http.Server{
		Addr:              addr,
		Handler:           server.Handler(),
		ReadHeaderTimeout: 10 * time.Second,
		// Generous, because a first request has to decompress the gazetteer
		// and parse four megabytes of planetary coefficients.
		WriteTimeout: 60 * time.Second,
		IdleTimeout:  120 * time.Second,
	}

	errc := make(chan error, 1)
	go func() {
		logger.Printf("listening on http://%s", addr)
		if online {
			logger.Printf("online place search is enabled")
		}
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errc <- err
		}
	}()

	if open {
		go openBrowser("http://" + addr)
	}

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt, syscall.SIGTERM)

	select {
	case err := <-errc:
		return err
	case <-stop:
		logger.Print("shutting down")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	return srv.Shutdown(ctx)
}

// openBrowser asks the desktop to open a URL. A failure is not worth reporting
// — the address is already on the console.
func openBrowser(url string) {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "windows":
		cmd = exec.Command("rundll32", "url.dll,FileProtocolHandler", url)
	case "darwin":
		cmd = exec.Command("open", url)
	default:
		cmd = exec.Command("xdg-open", url)
	}
	if err := cmd.Start(); err != nil {
		fmt.Fprintf(os.Stderr, "could not open a browser: %v\n", err)
	}
}
