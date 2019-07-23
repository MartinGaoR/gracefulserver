package main

import (
	"fmt"
	"net/http"
	"time"
)

func main() {
	// test mai
	success := make(chan struct{}, 60000)
	fail := make(chan struct{}, 60000)
	done := make(chan struct{}, 1)
	go sendRequests(success, fail, done)
	s, f := getResult(success, fail, done)

	fmt.Printf("Experiement done, total request sent: %d, success: %d, failed: %d, downtime: %dms\n", s + f, s, f, f)

}

func sendRequests(success, fail, done chan struct{}) {
	go func() {
		for i := 0; i < 60000; i ++  {
			go sendRequest(success, fail)
			time.Sleep(100 * time.Millisecond)
		}
	}()
	time.Sleep(60 * time.Second)
	close(done)

}

func sendRequest(success, fail chan struct{}) {
	resp, err := http.Get("http://localhost:59999/sleep/?duration=0s")
	if err != nil {
		fail <- struct{}{}
		return
	}
	if resp == nil {
		fail <- struct{}{}
		return
	}
	success <- struct{}{}
}

func getResult(success, fail, done chan struct{}) (int, int) {
	s, f := 0,0
	for {
		select {
		case <- success:
			s ++
			continue
		case <- fail:
			f ++
			continue
		case <- done:
			return s, f
		}
	}
}