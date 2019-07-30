package main

import (
	"fmt"
	"net/http"
	"os"
	"playground/gracefulserver/launcher/children/server"
	"time"
)

var (
	now = time.Now()
	// moodify
	version = "V3"
)

func main() {
	err := server.Serve(
		&http.Server{Addr: ":59999", Handler: newHandler("Zero  ")},
	)
	if err != nil {
		fmt.Println(err)
	}
}

func newHandler(name string) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/sleep/", func(w http.ResponseWriter, r *http.Request) {
		duration, err := time.ParseDuration(r.FormValue("duration"))
		if err != nil {
			http.Error(w, err.Error(), 400)
			return
		}
		time.Sleep(duration)
		fmt.Fprintf(
			w,
			"%s %s started at %s slept for %d nanoseconds from pid %d.\n",
			version,
			name,
			now,
			duration.Nanoseconds(),
			os.Getpid(),
		)
	})
	return mux
}