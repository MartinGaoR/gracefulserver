package main

import (
	"fmt"
	"io/ioutil"
	"math/rand"
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
		for i := 0; i < 1000; i ++  {
			go sendRequest(success, fail)
			time.Sleep(100 * time.Millisecond)
		}
	}()
	time.Sleep(4 * time.Minute)
	close(done)

}

func sendRequest(success, fail chan struct{}) {
	timeout := getTimeout()
	path := fmt.Sprintf("http://localhost:59999/sleep/?duration=%ds", timeout)
	resp, err := http.Get(path)
	if err != nil {
		fail <- struct{}{}
		return
	}
	if resp == nil {
		fail <- struct{}{}
		return
	}
	data, readErr := ioutil.ReadAll(resp.Body)
	defer func(){
		_ = resp.Body.Close()
	}()
	if readErr != nil {
		fail <- struct{}{}
		return
	}
	fmt.Println(string(data))
	success <- struct{}{}
}

func getTimeout() int {
	temp := rand.Int63n(100)
	switch temp%3 {
	case 0:
		return 30
	case 1:
		return 5
	case 2:
		return 0
	}
	// not possible
	return 0
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