// Copyright 2014 The Hanoverd Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package main

import (
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"net/http"
	"os"
	"os/signal"
	"path"
	"sync"
	"sync/atomic"

	"github.com/fsouza/go-dockerclient"
	"github.com/pwaller/barrier"
)

// DockerErrorStatus returns the HTTP status code represented by `err` or Status
// OK if no error or 0 if err != nil and is not a docker error.
func DockerErrorStatus(err error) int {
	if err, ok := err.(*docker.Error); ok {
		return err.Status
	}
	if err == nil {
		return http.StatusOK
	}
	return 0
}

func main() {
	log.Println("Hanoverd")

	client, err := docker.NewClient("unix:///run/docker.sock")
	if err != nil {
		log.Fatal(err)
	}

	var wg sync.WaitGroup
	defer wg.Wait()

	// Fired when we're signalled to exit
	var dying barrier.Barrier
	defer dying.Fall()

	go func() {
		defer dying.Fall()
		// Await Stdin closure
		io.Copy(ioutil.Discard, os.Stdin)
	}()

	Go := func(c *Container) {
		defer wg.Done()
		defer dying.Fall()

		go func() {
			for err := range c.Errors {
				log.Println("BUG: Async container error:", err)
			}
		}()

		status, err := c.Run()
		if err != nil {
			log.Println(err)
			dying.Fall()
			return
		}
		log.Println("exit:", status)
	}

	wd, err := os.Getwd()
	if err != nil {
		panic(err)
	}
	baseName := path.Base(wd)

	var i uint64
	getName := func() string {
		n := atomic.AddUint64(&i, 1)
		return fmt.Sprint(baseName, "_", n)
	}

	c := NewContainer(client, getName(), &wg, &dying)
	wg.Add(1)
	go Go(c)

	go func() {
		sig := make(chan os.Signal)
		signal.Notify(sig, os.Interrupt)
		for {
			<-sig
			log.Println("Signalled!")

			// NOTEs from messing with iptables proxying:
			// For external:
			// iptables -A PREROUTING -t nat -p tcp -m tcp --dport 5555 -j REDIRECT --to-ports 49278
			// For internal:
			// iptables -A OUTPUT -t nat -p tcp -m tcp --dport 5555 -j REDIRECT --to-ports 49278
			// To delete a rule, use -D rather than -A.

			d := NewContainer(client, getName(), &wg, &dying)
			wg.Add(1)
			go Go(d)
		}
	}()

	<-dying.Barrier()
}
