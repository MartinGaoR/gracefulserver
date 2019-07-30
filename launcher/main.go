package main

import (
	"fmt"
	"playground/gracefulserver/launcher/launcher"
	"sync"
	"time"
)

func main() {
	deploy := make(chan string, 10)
	done := make(chan struct{}, 1)
	err := make(chan error, 10)
	wg := &sync.WaitGroup{}
	srv := launcher.NewLauncher(wg, deploy, done, err)
	addListenerErr := srv.AddListener(":59999")
	if addListenerErr != nil {
		panic(addListenerErr)
	}
	go srv.Run()

	for i := 0; i < 3; i ++ {
		fmt.Println("deploying new version")
		deploy <- fmt.Sprintf("mainv%d", i)
		time.Sleep(45 * time.Second)
	}

	time.Sleep(time.Minute)

	done <- struct{}{}
	wg.Wait()
}