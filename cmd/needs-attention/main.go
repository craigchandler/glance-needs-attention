package main

import (
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"time"

	"github.com/craigchandler/glance-needs-attention/internal/app"
	cfgpkg "github.com/craigchandler/glance-needs-attention/internal/config"
)

var version = "dev"

func main() {
	configPath := flag.String("config", env("CONFIG_FILE", "/etc/needs-attention.json"), "configuration file")
	showVersion := flag.Bool("version", false, "print version")
	flag.Parse()

	if *showVersion {
		fmt.Println(version)
		return
	}

	cfg, err := cfgpkg.Load(*configPath)
	if err != nil {
		log.Fatal(err)
	}

	a := app.New(cfg)
	a.Start()

	mux := http.NewServeMux()
	mux.HandleFunc("/health", a.Health)
	mux.HandleFunc("/status", a.Status)
	mux.HandleFunc("/widget", a.Widget)

	log.Printf("needs-attention %s listening on %s using %s", version, cfg.ListenAddr, *configPath)
	log.Fatal(newServer(cfg.ListenAddr, mux).ListenAndServe())
}

func newServer(addr string, handler http.Handler) *http.Server {
	return &http.Server{
		Addr: addr, Handler: handler,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       10 * time.Second,
		WriteTimeout:      15 * time.Second,
		IdleTimeout:       60 * time.Second,
	}
}

func env(k, d string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return d
}
