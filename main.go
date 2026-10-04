package main

import (
	"embed"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"strings"

	"github.com/egoist/mygo"
	"port-kill/internal/ports"
)

//go:embed web/*
var assets embed.FS

type PortService struct{}

func (PortService) ListPorts() ([]ports.Entry, error)           { return ports.List() }
func (PortService) KillProcess(request ports.KillRequest) error { return ports.Kill(request) }

func page() string {
	html, _ := assets.ReadFile("web/index.html")
	css, _ := assets.ReadFile("web/style.css")
	js, _ := assets.ReadFile("web/app.js")
	return strings.NewReplacer("/* APP_CSS */", string(css), "/* APP_JS */", string(js)).Replace(string(html))
}

func main() {
	preview := flag.String("preview", "", "Read-only browser preview on a loopback address, e.g. 127.0.0.1:8787")
	snapshot := flag.Bool("snapshot", false, "Print current ports as JSON")
	flag.Parse()
	if *snapshot {
		rows, err := ports.List()
		if err != nil {
			log.Fatal(err)
		}
		json.NewEncoder(os.Stdout).Encode(rows)
		return
	}
	if *preview != "" {
		if !strings.HasPrefix(*preview, "127.0.0.1:") {
			log.Fatal("Preview must use 127.0.0.1")
		}
		mux := http.NewServeMux()
		mux.HandleFunc("GET /api/ports", func(w http.ResponseWriter, r *http.Request) {
			rows, err := ports.List()
			if err != nil {
				http.Error(w, err.Error(), 500)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(rows)
		})
		mux.HandleFunc("GET /", func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			fmt.Fprint(w, page())
		})
		log.Printf("Read-only preview: http://%s", *preview)
		log.Fatal(http.ListenAndServe(*preview, mux))
		return
	}
	mygo.App.SetName("PortPilot")
	mygo.Bind(PortService{})
	mygo.App.WhenReady(func() {
		win := mygo.NewWindow(mygo.WindowOptions{Title: "PortPilot · 端口管理器", Width: 1280, Height: 820, MinWidth: 860, MinHeight: 600, BackgroundColor: "#f6f8fb", StateKey: "main"})
		win.Page().LoadHTML(page(), "")
	})
	if err := mygo.App.Run(); err != nil {
		log.Fatal(err)
	}
}
