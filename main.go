package main

import (
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"sort"
	"sync"
	"time"

	"github.com/elastic/hey-apm/target"
	"github.com/graphaelli/hey/requester"
)

var (
	baseUrl            = flag.String("base-url", "http://localhost:8200/", "")
	runTimeout         = flag.Duration("run", 10*time.Second, "stop run after this duration")
	disableCompression = flag.Bool("disable-compression", false, "")
	disableKeepAlives  = flag.Bool("disable-keepalive", false, "")
	disableRedirects   = flag.Bool("disable-redirects", false, "")
	timeout            = flag.Int("timeout", 3, "request timeout")
)

// sortedTotal sorts the keys and sums the values of the input map
func sortedTotal(m map[int]int) ([]int, int) {
	keys := make([]int, len(m))
	i := 0
	total := 0
	for k, v := range m {
		keys[i] = k
		total += v
		i++
	}
	sort.Ints(keys)
	return keys, total
}

func printResults(work []*requester.Work, dur float64) {
	for i, w := range work {
		if i > 0 {
			fmt.Println()
		}

		statusCodeDist := w.StatusCodes()
		codes, total := sortedTotal(statusCodeDist)
		div := float64(total)
		fmt.Println(w.Request.URL, i)
		for _, code := range codes {
			cnt := statusCodeDist[code]
			fmt.Printf("  [%d]\t%d responses (%.2f%%) \n", code, cnt, 100*float64(cnt)/div)
		}
		fmt.Printf("  total\t%d responses (%.2f rps)\n", total, div/dur)

		errorDist := w.ErrorDist()
		for err, num := range errorDist {
			fmt.Printf("  [%d]\t%s\n", num, err)
		}
	}
}

func main() {
	flag.Parse()

	logger := log.New(os.Stderr, "", log.Ldate|log.Ltime|log.Lshortfile)

	work := target.Get(*baseUrl, &target.Config{
		RequestTimeout:     *timeout,
		DisableCompression: *disableCompression,
		DisableKeepAlives:  *disableKeepAlives,
		DisableRedirects:   *disableRedirects,
	})

	start := time.Now()
	var wg sync.WaitGroup
	wg.Add(len(work))
	for _, w := range work {
		go func(w *requester.Work) {
			logger.Println("[info] starting worker for", w.Request.URL)
			w.Run()
			logger.Println("[info] worker done for", w.Request.URL)
			wg.Done()
		}(w)
	}

	stopWorking := func() {
		for _, w := range work {
			go func(w *requester.Work) {
				logger.Println("[info] stopping worker for", w.Request.URL)
				w.Stop()
			}(w)
		}
	}

	// stop working on signal
	c := make(chan os.Signal, 1)
	signal.Notify(c, os.Interrupt)
	go func() {
		<-c
		logger.Println("[error] caught signal, stopping work")
		stopWorking()
	}()

	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		logger.Println("[info] no more requests to make")
	case <-time.After(*runTimeout):
		logger.Println("[info] no more time left to make requests")
		stopWorking()
		select {
		case <-done:
			logger.Println("[info] stopped cleanly after time expired")
		case <-time.After(10 * time.Second):
			logger.Println("[error] failed to stop cleanly after timeout, aborting")
		}
	}

	printResults(work, time.Now().Sub(start).Seconds())
}