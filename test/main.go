package main

import (
	"fmt"
	"io/ioutil"
	"math/rand"
	"net/http"
	"time"
)

func main() {
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
			go sendReq("http://localhost:59999/sleep/?duration=%ds", getTimeout(), success, fail)
			time.Sleep(100 * time.Millisecond)
		}
	}()

	go func() {
		for i := 0; i < 1000; i ++  {
			// a more randomized timeout in the second endpoint
			go sendReq("http://localhost:60000/sleep/?duration=%ds", rand.Intn(30), success, fail)
			time.Sleep(100 * time.Millisecond)
		}
	}()

	time.Sleep(1 * time.Minute)
	close(done)

}

func sendReq(path string, timeout int, success, fail chan struct{}) {
	p := fmt.Sprintf(path, timeout)
	resp, err := http.Get(p)
	if err != nil {
		fmt.Println(err)
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